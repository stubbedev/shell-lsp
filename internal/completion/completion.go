package completion

import (
	context "context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
)

const (
	maxExecutables = 2000
	maxPaths       = 1000
)

type Kind uint8

const (
	KindKeyword Kind = iota
	KindBuiltin
	KindFunction
	KindAlias
	KindCommand
	KindVariable
	KindFile
	KindFolder
	KindFlag
	KindOption
)

type DirEntry struct {
	Name string
	Dir  bool
}

type Environment interface {
	Executables() []string
	ReadDir(dir string) ([]DirEntry, error)
	Home() string
	EnvironmentNames() []string
	Options(ctx context.Context, command string) ([]manpage.Option, error)
}

type Input struct {
	Tree      *parser.Tree
	Offset    int
	ScriptDir string
	WorkDir   string
	Snippets  bool
	Env       Environment
}

type Item struct {
	Label         string
	Kind          Kind
	Detail        string
	Documentation string
	SortText      string
	InsertText    string
	Snippet       bool
	Replace       parser.Span
	Resolve       bool
}

type Result struct {
	Items      []Item
	Incomplete bool
}

func Complete(ctx context.Context, in Input) (Result, error) {
	c := completer{Input: in, dialect: in.Tree.Dialect, site: analyze(in.Tree, in.Offset)}
	switch c.site.position {
	case positionVariable:
		return Result{Items: c.variables()}, nil
	case positionCommand:
		if strings.ContainsAny(c.site.prefix, "/~") {
			return c.paths(false)
		}
		return c.commands(true), nil
	case positionArgument:
		return c.argument(ctx)
	}
	return Result{}, nil
}

type completer struct {
	Input
	dialect dialect.Dialect
	site    site
}

func (c completer) item(label string, kind Kind, sort string) Item {
	return Item{Label: label, Kind: kind, SortText: sort + label, InsertText: label, Replace: c.site.replace}
}

func (c completer) commands(withKeywords bool) Result {
	var items []Item
	seen := map[string]bool{}
	add := func(it Item) {
		if seen[it.Label] || !strings.HasPrefix(it.Label, c.site.prefix) {
			return
		}
		seen[it.Label] = true
		items = append(items, it)
	}
	for _, fn := range c.Tree.Functions {
		it := c.item(fn.Name, KindFunction, "1")
		it.Detail = "function"
		it.Documentation = hover.FunctionDoc(c.Tree, fn)
		add(it)
	}
	for _, alias := range c.aliases() {
		it := c.item(alias.name, KindAlias, "1")
		it.Detail = "alias for " + alias.value
		add(it)
	}
	if withKeywords {
		for _, e := range shelldocs.Keywords(c.dialect) {
			add(c.keyword(e))
		}
	}
	for _, e := range shelldocs.Builtins(c.dialect) {
		it := c.item(e.Name, KindBuiltin, "2")
		it.Detail = "builtin"
		it.Documentation = e.Markdown(c.dialect)
		add(it)
	}
	executables := c.Env.Executables()
	count := 0
	incomplete := false
	for _, name := range executables {
		if !strings.HasPrefix(name, c.site.prefix) || seen[name] {
			continue
		}
		if count == maxExecutables {
			incomplete = true
			break
		}
		count++
		it := c.item(name, KindCommand, "3")
		it.Detail = "command"
		it.Resolve = true
		add(it)
	}
	return Result{Items: items, Incomplete: incomplete}
}

var snippets = map[string]string{
	"if":       "if ${1:condition}; then\n\t$0\nfi",
	"for":      "for ${1:name} in ${2:words}; do\n\t$0\ndone",
	"select":   "select ${1:name} in ${2:words}; do\n\t$0\ndone",
	"while":    "while ${1:condition}; do\n\t$0\ndone",
	"until":    "until ${1:condition}; do\n\t$0\ndone",
	"case":     "case ${1:word} in\n\t${2:pattern})\n\t\t$0\n\t\t;;\nesac",
	"function": "function ${1:name} {\n\t$0\n}",
}

func (c completer) keyword(e shelldocs.Entry) Item {
	it := c.item(e.Name, KindKeyword, "2")
	it.Detail = "keyword"
	it.Documentation = e.Markdown(c.dialect)
	if snippet, ok := snippets[e.Name]; ok && c.Snippets {
		it.InsertText = snippet
		it.Snippet = true
	}
	return it
}

type alias struct {
	name, value string
}

func (c completer) aliases() []alias {
	var out []alias
	for _, cmd := range c.Tree.Commands {
		if cmd.Name != "alias" {
			continue
		}
		for _, arg := range cmd.Args {
			value, ok := parser.Literal(arg)
			if !ok {
				continue
			}
			name, body, found := strings.Cut(value, "=")
			if found && name != "" && !strings.HasPrefix(name, "-") {
				out = append(out, alias{name: name, value: body})
			}
		}
	}
	return out
}

var commandTakers = map[string]bool{
	"command": true, "type": true, "which": true, "whence": true, "where": true, "sudo": true, "doas": true,
	"exec": true, "nohup": true, "env": true, "xargs": true, "man": true, "builtin": true, "run-help": true,
	"time": true, "noglob": true,
}

func (c completer) argument(ctx context.Context) (Result, error) {
	last := ""
	if n := len(c.site.previous); n > 0 {
		last = c.site.previous[n-1]
	}
	switch {
	case c.site.command == "set" && (last == "-o" || last == "+o"):
		return Result{Items: c.options(shelldocs.SetOptions)}, nil
	case c.site.command == "shopt":
		return Result{Items: c.options(shelldocs.Shopt)}, nil
	case c.site.command == "setopt" || c.site.command == "unsetopt":
		return Result{Items: c.options(shelldocs.Setopt)}, nil
	case strings.HasPrefix(c.site.prefix, "-"):
		return c.flags(ctx)
	case c.site.command == "unset" || c.site.command == "export" || c.site.command == "readonly":
		return Result{Items: c.variables()}, nil
	case commandTakers[c.site.command] && !strings.Contains(c.site.prefix, "/"):
		return c.commands(false), nil
	case c.site.command == "cd" || c.site.command == "pushd":
		return c.paths(true)
	}
	return c.paths(false)
}

func (c completer) options(set shelldocs.OptionSet) []Item {
	var items []Item
	for _, o := range shelldocs.Options(set, c.dialect) {
		if !strings.HasPrefix(o.Name, c.site.prefix) {
			continue
		}
		it := c.item(o.Name, KindOption, "1")
		it.Documentation = o.Doc
		items = append(items, it)
	}
	return items
}

func (c completer) flags(ctx context.Context) (Result, error) {
	options, err := c.Env.Options(ctx, c.site.command)
	if errors.Is(err, manpage.ErrNoPage) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for i, opt := range options {
		for _, name := range opt.Names {
			if !strings.HasPrefix(name, c.site.prefix) {
				continue
			}
			it := c.item(name, KindFlag, fmt.Sprintf("%04d", i))
			it.Detail = strings.Join(opt.Names, ", ")
			it.Documentation = opt.Doc
			items = append(items, it)
		}
	}
	return Result{Items: items}, nil
}

func (c completer) variables() []Item {
	var items []Item
	seen := map[string]bool{}
	scope := c.Tree.ScopeAt(c.Offset)
	add := func(it Item) {
		if seen[it.Label] || !strings.HasPrefix(it.Label, c.site.prefix) {
			return
		}
		seen[it.Label] = true
		items = append(items, it)
	}
	for _, def := range c.Tree.Definitions {
		if seen[def.Name] {
			continue
		}
		visible := c.Tree.Visible(def.Name, scope)
		if len(visible) == 0 {
			continue
		}
		sort := "1"
		detail := "global"
		if visible[0].Local {
			sort, detail = "0", "local"
		}
		it := c.item(def.Name, KindVariable, sort)
		it.Detail = detail
		it.Documentation = hover.VariableDoc(c.Tree, c.Offset, def.Name)
		add(it)
	}
	for _, e := range shelldocs.Variables(c.dialect) {
		it := c.item(e.Name, KindVariable, "2")
		it.Detail = "shell variable"
		it.Documentation = e.Markdown(c.dialect)
		add(it)
	}
	for _, name := range c.Env.EnvironmentNames() {
		it := c.item(name, KindVariable, "3")
		it.Detail = "environment"
		add(it)
	}
	if c.site.prefix == "" {
		for _, name := range []string{"@", "*", "#", "?", "$", "!", "0", "-", "_"} {
			entry, ok := shelldocs.Parameter(name)
			if !ok || !entry.Dialects.Has(c.dialect) {
				continue
			}
			it := c.item(name, KindVariable, "4")
			it.Detail = "special parameter"
			it.Documentation = entry.Doc
			add(it)
		}
	}
	return items
}

func (c completer) paths(dirsOnly bool) (Result, error) {
	raw := c.site.prefix
	quote := ""
	if raw != "" && (raw[0] == '"' || raw[0] == '\'') {
		quote, raw = raw[:1], raw[1:]
	}
	dirPart, base := "", raw
	if i := strings.LastIndexByte(raw, '/'); i >= 0 {
		dirPart, base = raw[:i+1], raw[i+1:]
	}
	var items []Item
	seen := map[string]bool{}
	incomplete := false
	for _, root := range c.roots(dirPart) {
		entries, err := c.Env.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("listing %s for path completion: %w", root, err)
		}
		for _, e := range entries {
			if seen[e.Name] || !strings.HasPrefix(e.Name, base) || (dirsOnly && !e.Dir) {
				continue
			}
			if strings.HasPrefix(e.Name, ".") && !strings.HasPrefix(base, ".") {
				continue
			}
			if len(items) == maxPaths {
				incomplete = true
				break
			}
			seen[e.Name] = true
			items = append(items, c.pathItem(e, quote, dirPart))
		}
	}
	return Result{Items: items, Incomplete: incomplete}, nil
}

func (c completer) pathItem(e DirEntry, quote, dirPart string) Item {
	label, kind, sort := e.Name, KindFile, "1"
	if e.Dir {
		label, kind, sort = e.Name+"/", KindFolder, "0"
	}
	it := c.item(label, kind, sort)
	it.InsertText = quote + dirPart + escapeName(label, quote)
	return it
}

func (c completer) roots(dirPart string) []string {
	home := c.Env.Home()
	switch {
	case dirPart == "~/" || strings.HasPrefix(dirPart, "~/"):
		return []string{filepath.Join(home, dirPart[2:])}
	case strings.HasPrefix(dirPart, "$HOME/"):
		return []string{filepath.Join(home, dirPart[len("$HOME/"):])}
	case filepath.IsAbs(dirPart):
		return []string{dirPart}
	}
	var roots []string
	for _, base := range []string{c.cdTarget(), c.ScriptDir, c.WorkDir} {
		if base == "" {
			continue
		}
		root := filepath.Join(base, dirPart)
		if !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}
	return roots
}

func (c completer) cdTarget() string {
	target := ""
	for _, cmd := range c.Tree.Commands {
		if cmd.Name != "cd" || cmd.Span.End > c.Offset || len(cmd.Args) != 1 {
			continue
		}
		dir, ok := parser.Literal(cmd.Args[0])
		if !ok {
			continue
		}
		if strings.HasPrefix(dir, "~/") {
			dir = filepath.Join(c.Env.Home(), dir[2:])
		}
		if filepath.IsAbs(dir) {
			target = dir
		}
	}
	return target
}

func escapeName(name, quote string) string {
	var special string
	switch quote {
	case "'":
		return strings.ReplaceAll(name, "'", `'\''`)
	case `"`:
		special = "\"$`\\"
	default:
		special = " \t'\"$`\\&;|()<>*?[]{}!"
	}
	var sb strings.Builder
	for _, r := range name {
		if strings.ContainsRune(special, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
