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
	KindSubcommand
)

type DirEntry struct {
	Name string
	Dir  bool
}

type Environment interface {
	Executables() []string
	ReadDir(dir string) ([]DirEntry, error)
	Home() string
	WorkDir() (string, error)
	EnvironmentNames() []string
	Options(ctx context.Context, command string, args []string) ([]manpage.Option, error)
	Subcommands(ctx context.Context, command string, args []string) (manpage.Listing, error)
}

type Input struct {
	Tree      *parser.Tree
	Imports   []parser.Import
	Offset    int
	ScriptDir string
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
	Resolve       ResolveKind
}

type ResolveKind uint8

const (
	ResolveNone ResolveKind = iota
	ResolveCommand
	ResolveFunction
	ResolveVariable
)

func Documentation(tree *parser.Tree, imports []parser.Import, kind ResolveKind, name string, offset int) (string, bool) {
	switch kind {
	case ResolveFunction:
		if fn, ok := tree.FunctionNamed(name); ok {
			return hover.FunctionDoc(tree, fn), true
		}
		if imp, fn, ok := parser.ImportedFunction(imports, name); ok {
			return hover.ImportedFunctionDoc(imp, fn), true
		}
		return "", false
	case ResolveVariable:
		return hover.VariableDoc(tree, imports, min(max(offset, 0), len(tree.Text())), name), true
	}
	return "", false
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
		it.Resolve = ResolveFunction
		add(it)
	}
	for _, imp := range c.Imports {
		for _, fn := range imp.Tree.Functions {
			it := c.item(fn.Name, KindFunction, "1")
			it.Detail = "function from " + imp.Name
			it.Resolve = ResolveFunction
			add(it)
		}
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
		it.Resolve = ResolveCommand
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
	"type": true, "which": true, "whence": true, "where": true, "man": true, "run-help": true, "noglob": true,
}

func (c completer) argument(ctx context.Context) (Result, error) {
	wrappers := parser.Wrappers{Bats: c.Tree.Bats(), Programs: true}
	command, args := c.site.command, c.site.previous
	for {
		wrap, wrapped := wrappers.Unwrap(command, args)
		if !wrapped {
			return c.argumentOf(ctx, command, args)
		}
		if wrap.Command == len(args) {
			return c.wrapperArgument(ctx, command, args, wrap)
		}
		command, args = args[wrap.Command], args[wrap.Command+1:]
	}
}

func (c completer) wrapperArgument(ctx context.Context, wrapper string, args []string, wrap parser.Wrap) (Result, error) {
	switch {
	case strings.HasPrefix(c.site.prefix, "-"):
		return c.flags(ctx, wrapper, args)
	case wrap.Pending || strings.Contains(c.site.prefix, "/"):
		return c.paths(false)
	}
	return c.commands(false), nil
}

func (c completer) argumentOf(ctx context.Context, command string, args []string) (Result, error) {
	last := ""
	if n := len(args); n > 0 {
		last = args[n-1]
	}
	switch {
	case command == "set" && (last == "-o" || last == "+o"):
		return Result{Items: c.options(shelldocs.SetOptions)}, nil
	case command == "shopt":
		return Result{Items: c.options(shelldocs.ShoptOptions)}, nil
	case command == "setopt" || command == "unsetopt":
		return Result{Items: c.options(shelldocs.SetoptOptions)}, nil
	case strings.HasPrefix(c.site.prefix, "-"):
		return c.flags(ctx, command, args)
	case command == "unset" || command == "export" || command == "readonly":
		return Result{Items: c.variables()}, nil
	case commandTakers[command] && !strings.Contains(c.site.prefix, "/"):
		return c.commands(false), nil
	case command == "cd" || command == "pushd":
		return c.paths(true)
	}
	return c.positional(ctx, command, args)
}

func (c completer) positional(ctx context.Context, command string, args []string) (Result, error) {
	listing, err := c.Env.Subcommands(ctx, command, args)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, sub := range listing.Subcommands {
		if !strings.HasPrefix(sub.Name, c.site.prefix) {
			continue
		}
		it := c.item(sub.Name, KindSubcommand, "0")
		it.Detail = sub.Summary
		it.Documentation = sub.Doc
		items = append(items, it)
	}
	incomplete := len(listing.Unloaded) > 0
	if len(items) > 0 {
		return Result{Items: items, Incomplete: incomplete}, nil
	}
	paths, err := c.paths(false)
	paths.Incomplete = paths.Incomplete || incomplete
	return paths, err
}

func (c completer) options(set shelldocs.Vocabulary) []Item {
	var items []Item
	for o := range shelldocs.Terms(set, c.dialect) {
		if !strings.HasPrefix(o.Name, c.site.prefix) {
			continue
		}
		it := c.item(o.Name, KindOption, "1")
		it.Documentation = o.Doc
		items = append(items, it)
	}
	return items
}

func (c completer) flags(ctx context.Context, command string, args []string) (Result, error) {
	options, err := c.Env.Options(ctx, command, args)
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
		if seen[def.Name] || !strings.HasPrefix(def.Name, c.site.prefix) {
			continue
		}
		visible := c.Tree.Visible(def.Name, scope)
		if visible.Len() == 0 {
			continue
		}
		sort := "1"
		detail := "global"
		if visible.At(0).Local {
			sort, detail = "0", "local"
		}
		it := c.item(def.Name, KindVariable, sort)
		it.Detail = detail
		it.Resolve = ResolveVariable
		add(it)
	}
	for _, imp := range c.Imports {
		for _, def := range imp.Tree.Definitions {
			if def.Local || seen[def.Name] || !strings.HasPrefix(def.Name, c.site.prefix) {
				continue
			}
			it := c.item(def.Name, KindVariable, "1")
			it.Detail = "global from " + imp.Name
			it.Resolve = ResolveVariable
			add(it)
		}
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
	roots, err := c.roots(dirPart)
	if err != nil {
		return Result{}, err
	}
	for _, root := range roots {
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

func (c completer) roots(dirPart string) ([]string, error) {
	switch {
	case strings.HasPrefix(dirPart, "~/"):
		return []string{filepath.Join(c.Env.Home(), dirPart[2:])}, nil
	case strings.HasPrefix(dirPart, "$HOME/"):
		return []string{filepath.Join(c.Env.Home(), dirPart[len("$HOME/"):])}, nil
	case filepath.IsAbs(dirPart):
		return []string{dirPart}, nil
	}
	workDir, err := c.Env.WorkDir()
	if err != nil {
		return nil, fmt.Errorf("resolving the working directory for path completion: %w", err)
	}
	var roots []string
	for _, base := range []string{c.cdTarget(), c.ScriptDir, workDir} {
		if base == "" {
			continue
		}
		root := filepath.Join(base, dirPart)
		if !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}
	return roots, nil
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
