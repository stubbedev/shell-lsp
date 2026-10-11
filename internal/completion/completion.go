package completion

import (
	context "context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
	KindOperator
	KindSignal
	KindValue
	KindKey
	KindSnippet
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
	Trigger   string
	Env       Environment
}

type Item struct {
	Label         string
	Kind          Kind
	Detail        string
	Documentation string
	SortText      string
	InsertText    string
	FilterText    string
	Snippet       bool
	Replace       parser.Span
	Word          parser.Span
	LabelDetail   string
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
	if result, ok, err := directiveCompletion(in); ok {
		return result, err
	}
	if directiveTrigger(in.Trigger) {
		return Result{}, nil
	}
	c := completer{Input: in, dialect: in.Tree.Dialect, site: analyze(in.Tree, in.Offset)}
	c.site.wordEnd = tokenEnd(in.Tree.Text(), c.site)
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
	case positionExpansionOperator:
		return Result{Items: c.terms(shelldocs.ExpansionOperators, KindOperator)}, nil
	case positionParameterFlags:
		return Result{Items: c.letters(shelldocs.ZshParameterFlags)}, nil
	case positionGlobQualifier:
		return Result{Items: c.letters(shelldocs.ZshGlobQualifiers)}, nil
	case positionArrayKey:
		return Result{Items: c.arrayKeys()}, nil
	case positionCasePattern:
		return Result{Items: c.casePatterns()}, nil
	}
	return Result{}, nil
}

func formatArgument(args []string) bool {
	for len(args) > 0 {
		switch args[0] {
		case "--":
			return len(args) == 1
		case "-v":
			if len(args) < 2 {
				return false
			}
			args = args[2:]
		default:
			return false
		}
	}
	return true
}

var conversionSpec = regexp.MustCompile(`^[-+ #0]*([0-9]+|\*)?(\.([0-9]+|\*)?)?$`)

func (c completer) printfDirectives() []Item {
	prefix := c.site.prefix
	i := strings.LastIndexByte(prefix, '%')
	if i < 0 || !conversionSpec.MatchString(prefix[i+1:]) {
		return nil
	}
	run := 1
	for j := i - 1; j >= 0 && prefix[j] == '%'; j-- {
		run++
	}
	if run%2 == 0 {
		return nil
	}
	at := parser.Span{Start: c.Offset, End: c.Offset}
	var items []Item
	for e := range shelldocs.Terms(shelldocs.PrintfDirectives, c.dialect) {
		label := e.Name
		if e.Synopsis != "" {
			label = e.Synopsis
		}
		it := c.item(label, KindValue, "1")
		it.InsertText = label[1:]
		it.Replace, it.Word = at, at
		it.Documentation = e.Doc
		items = append(items, it)
	}
	return items
}

func (c completer) letters(v shelldocs.Vocabulary) []Item {
	var items []Item
	for e := range shelldocs.Terms(v, c.dialect) {
		if len(e.Name) == 1 && strings.Contains(c.site.typed, e.Name) && !repeatable(v, e.Name) {
			continue
		}
		it := c.item(e.Name, KindFlag, "1")
		it.Documentation = e.Doc
		items = append(items, it)
	}
	return items
}

func repeatable(v shelldocs.Vocabulary, name string) bool {
	return (v == shelldocs.ZshParameterFlags && name == "q") || v == shelldocs.ZshGlobQualifiers
}

func (c completer) arrayKeys() []Item {
	var items []Item
	seen := map[string]bool{}
	add := func(key, detail, sort string) {
		if seen[key] || !strings.HasPrefix(key, c.site.prefix) {
			return
		}
		seen[key] = true
		it := c.item(key, KindKey, sort)
		it.Detail = detail
		items = append(items, it)
	}
	for _, key := range c.Tree.LiteralKeys(c.site.name) {
		add(key, "key of "+c.site.name, "0")
	}
	add("@", "every element, as separate words when quoted", "1")
	add("*", "every element, joined with the first character of IFS when quoted", "1")
	return items
}

type completer struct {
	Input
	dialect dialect.Dialect
	site    site
}

func (c completer) item(label string, kind Kind, sort string) Item {
	return Item{
		Label:      label,
		Kind:       kind,
		SortText:   sort + label,
		InsertText: label,
		Replace:    c.site.replace,
		Word:       parser.Span{Start: c.site.replace.Start, End: c.site.wordEnd},
	}
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
		for _, it := range c.idioms() {
			add(it)
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

func (c completer) keyword(e shelldocs.Entry) Item {
	it := c.item(e.Name, KindKeyword, "2")
	it.Detail = "keyword"
	it.Documentation = e.Markdown(c.dialect)
	if snippet, ok := keywordSnippets[e.Name]; ok && c.Snippets {
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
	if command == "[[" || command == "[" || command == "test" {
		if items, ok := c.testExpression(command, args); ok {
			return Result{Items: items}, nil
		}
	}
	switch {
	case command == "set" && (last == "-o" || last == "+o"):
		return Result{Items: c.options(shelldocs.SetOptions)}, nil
	case command == "kill" && last == "-s":
		return Result{Items: c.signals("", shelldocs.Signals)}, nil
	case command == "kill" && strings.HasPrefix(c.site.prefix, "-"):
		flags, err := c.flags(ctx, command, args)
		flags.Items = append(flags.Items, c.signals("-", shelldocs.Signals)...)
		return flags, err
	case command == "trap" && len(args) > 0 && !strings.HasPrefix(c.site.prefix, "-"):
		return Result{Items: c.signals("", shelldocs.TrapConditions, shelldocs.Signals)}, nil
	case command == "printf" && formatArgument(args):
		return Result{Items: c.printfDirectives()}, nil
	case command == "emulate" && !strings.HasPrefix(c.site.prefix, "-"):
		return Result{Items: c.terms(shelldocs.EmulateModes, KindValue)}, nil
	case strings.HasPrefix(c.site.prefix, "-"):
		return c.flags(ctx, command, args)
	case command == "shopt":
		return Result{Items: c.options(shelldocs.ShoptOptions)}, nil
	case command == "setopt" || command == "unsetopt":
		return Result{Items: c.options(shelldocs.SetoptOptions)}, nil
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

type testState uint8

const (
	testStart testState = iota
	testAfterUnary
	testAfterOperand
	testAfterBinary
	testDone
)

func (c completer) testExpression(command string, args []string) ([]Item, bool) {
	state, last := testPosition(command, args)
	switch state {
	case testStart:
		if !strings.HasPrefix(c.site.prefix, "-") {
			return nil, false
		}
		return c.terms(shelldocs.TestUnaryOperators, KindOperator), true
	case testAfterOperand:
		items := c.terms(shelldocs.TestBinaryOperators, KindOperator)
		if command == "[[" {
			items = append(items, c.terms(shelldocs.CondOperators, KindOperator)...)
		}
		return items, true
	case testAfterUnary:
		switch last {
		case "-v", "-R":
			return c.variables(), true
		case "-o":
			if command == "[[" {
				return c.options(shelldocs.SetOptions), true
			}
		}
		return nil, false
	case testDone:
		return nil, true
	}
	return nil, false
}

func testPosition(command string, args []string) (testState, string) {
	connector := func(w string) bool {
		if command == "[[" {
			return w == "&&" || w == "||"
		}
		return w == "-a" || w == "-o"
	}
	state, last := testStart, ""
	for _, w := range args {
		last = w
		switch {
		case w == "]]" || w == "]" || w == ")":
			state = testDone
			continue
		case state == testStart && (w == "!" || w == "("):
			continue
		}
		switch state {
		case testStart:
			state = testAfterOperand
			if _, ok := shelldocs.Term(shelldocs.TestUnaryOperators, w); ok {
				state = testAfterUnary
			}
		case testAfterOperand:
			switch {
			case connector(w):
				state = testStart
			case isTestBinary(w):
				state = testAfterBinary
			default:
				state = testDone
			}
		case testAfterUnary, testAfterBinary:
			state = testDone
		case testDone:
			if connector(w) {
				state = testStart
			}
		}
	}
	return state, last
}

func isTestBinary(w string) bool {
	if _, ok := shelldocs.Term(shelldocs.TestBinaryOperators, w); ok {
		return true
	}
	_, ok := shelldocs.Term(shelldocs.CondOperators, w)
	return ok && w != "&&" && w != "||"
}

func (c completer) terms(v shelldocs.Vocabulary, kind Kind) []Item {
	var items []Item
	for e := range shelldocs.Terms(v, c.dialect) {
		if !strings.HasPrefix(e.Name, c.site.prefix) {
			continue
		}
		it := c.item(e.Name, kind, "1")
		it.Detail = e.Synopsis
		it.Documentation = e.Doc
		if suffix, ok := strings.CutPrefix(e.Synopsis, e.Name); ok {
			it.LabelDetail = suffix
		}
		items = append(items, it)
	}
	return items
}

func (c completer) signals(labelPrefix string, vocabularies ...shelldocs.Vocabulary) []Item {
	typed := strings.ToUpper(strings.TrimPrefix(c.site.prefix, labelPrefix))
	typed, spelledSIG := strings.CutPrefix(typed, "SIG")
	var items []Item
	for i, v := range vocabularies {
		for e := range shelldocs.Terms(v, c.dialect) {
			if !strings.HasPrefix(e.Name, typed) {
				continue
			}
			it := c.item(labelPrefix+e.Name, KindSignal, strconv.Itoa(i))
			if v == shelldocs.Signals {
				it.Detail = "SIG" + e.Name
			}
			it.Documentation = e.Doc
			it.FilterText = labelPrefix + e.Name
			if spelledSIG && v == shelldocs.Signals {
				it.FilterText = labelPrefix + "SIG" + e.Name
			}
			items = append(items, it)
		}
	}
	return items
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
	if flags, ok := shelldocs.BuiltinFlags(command, c.dialect); ok {
		var items []Item
		for e := range flags {
			if !strings.HasPrefix(e.Name, c.site.prefix) {
				continue
			}
			it := c.item(e.Name, KindFlag, "0")
			it.Detail = command + " " + e.Name
			it.Documentation = e.Doc
			items = append(items, it)
		}
		return Result{Items: items}, nil
	}
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
