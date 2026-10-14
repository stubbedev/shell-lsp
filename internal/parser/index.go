package parser

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type ScopeID int

const FileScope ScopeID = 0

type Scope struct {
	Span   Span
	Parent ScopeID
}

type ShellID int

const FileShell ShellID = 0

type Shell struct {
	Span   Span
	Parent ShellID
}

type Function struct {
	Name      string
	NameSpan  Span
	Span      Span
	Body      ScopeID
	Within    ScopeID
	Namespace string
	Doc       string
	Decl      *syntax.FuncDecl
}

func (f Function) NamesArgZero(d dialect.Dialect) bool {
	switch d {
	case dialect.Zsh:
		return true
	case dialect.Ksh:
		return f.Decl != nil && f.Decl.RsrvWord
	case dialect.Bash, dialect.Posix, dialect.Dash, dialect.Busybox:
		return false
	}
	panic(fmt.Sprintf("parser: unknown dialect %d", d))
}

type DefinitionKind uint8

const (
	DefAssign DefinitionKind = iota
	DefCommandEnv
	DefDeclare
	DefLoop
	DefArithmetic
	DefBuiltin
	DefDescriptor
)

type Attr uint8

const (
	AttrArray Attr = 1 << iota
	AttrAssoc
	AttrExport
	AttrReadonly
	AttrInteger
	AttrNameref
	AttrTransformed
)

func (a Attr) Has(flag Attr) bool { return a&flag != 0 }

type Definition struct {
	Name     string
	NameSpan Span
	Span     Span
	Kind     DefinitionKind
	Attrs    Attr
	Cleared  Attr
	Local    bool
	Within   ScopeID
	Value    *syntax.Word
	Node     syntax.Node
}

type Reference struct {
	Name   string
	Span   Span
	Within ScopeID
}

type Command struct {
	Name     string
	NameSpan Span
	Span     Span
	Args     []*syntax.Word
	Within   ScopeID
}

type Redirect struct {
	Op     syntax.RedirOperator
	OpSpan Span
	Span   Span
	Target Span
}

type HeredocStrip uint8

const (
	StripNone HeredocStrip = iota
	StripTabs
	StripBlanks
)

func (s HeredocStrip) String() string {
	switch s {
	case StripNone:
		return "none"
	case StripTabs:
		return "tabs"
	case StripBlanks:
		return "blanks"
	}
	panic(fmt.Sprintf("parser: unknown here-document strip %d", s))
}

func IsHeredoc(op syntax.RedirOperator) bool {
	switch op {
	case syntax.Hdoc, syntax.DashHdoc, syntax.HashHdoc:
		return true
	}
	return false
}

func heredocStrip(op syntax.RedirOperator) HeredocStrip {
	switch op {
	case syntax.DashHdoc:
		return StripTabs
	case syntax.HashHdoc:
		return StripBlanks
	}
	return StripNone
}

type Heredoc struct {
	Delimiter string
	Quoted    bool
	Strip     HeredocStrip
	Span      Span
	Body      Span
	Redirect  *syntax.Redirect
}

type Comment struct {
	Span Span
	Text string
}

type Keyword struct {
	Name string
	Span Span
}

type Test struct {
	Name     string
	Segments []string
	NameSpan Span
	Span     Span
}

func (t Test) Literal() bool { return len(t.Segments) == 1 }

type Index struct {
	Scopes      []Scope
	Shells      []Shell
	Functions   []Function
	Definitions []Definition
	References  []Reference
	Commands    []Command
	Redirects   []Redirect
	Heredocs    []Heredoc
	Comments    []Comment
	Keywords    []Keyword
	Tests       []Test
	Statements  int

	definitionsByBinding map[binding][]int
	attrsByBinding       map[binding]Attr
	attrsByName          map[string]Attr
	definitionsByScope   map[ScopeID][]int
	localScopes          map[string]map[ScopeID]bool
	functionsByName      map[string]int
	functionsByBody      map[ScopeID]int
}

type binding struct {
	name  string
	scope ScopeID
	local bool
}

type builder struct {
	text        string
	index       Index
	errs        []Error
	stack       []syntax.Node
	scopes      []ScopeID
	shells      []shellOpening
	elseClauses map[*syntax.IfClause]bool
	closedAtEnd map[*syntax.Redirect]bool
	compounds   int
	namespaces  []string
	lang        syntax.LangVariant
	dialect     dialect.Dialect
	features    dialect.Features
}

func buildIndex(file *syntax.File, lang syntax.LangVariant, d dialect.Dialect, text string, closedAtEnd map[*syntax.Redirect]bool, previous *Tree) (Index, []Error) {
	b := &builder{
		lang:        lang,
		dialect:     d,
		features:    d.Features(),
		text:        text,
		closedAtEnd: closedAtEnd,
		index:       presized(previous),
		scopes:      []ScopeID{FileScope},
		elseClauses: map[*syntax.IfClause]bool{},
	}
	b.index.Scopes = append(b.index.Scopes, Scope{Span: Span{Start: 0, End: len(text)}, Parent: FileScope})
	b.index.Shells = append(b.index.Shells, Shell{Span: Span{Start: 0, End: len(text)}, Parent: FileShell})
	b.shells = []shellOpening{{id: FileShell}}
	Walk(file, b.visit)
	slices.SortStableFunc(b.index.Keywords, func(x, y Keyword) int { return x.Span.Start - y.Span.Start })
	slices.SortStableFunc(b.index.Comments, func(x, y Comment) int { return x.Span.Start - y.Span.Start })
	b.index.buildLookups()
	return b.index, b.errs
}

func presized(previous *Tree) Index {
	if previous == nil {
		return Index{}
	}
	p := previous.Index
	return Index{
		Scopes:      withRoomFor(p.Scopes),
		Shells:      withRoomFor(p.Shells),
		Functions:   withRoomFor(p.Functions),
		Definitions: withRoomFor(p.Definitions),
		References:  withRoomFor(p.References),
		Commands:    withRoomFor(p.Commands),
		Redirects:   withRoomFor(p.Redirects),
		Heredocs:    withRoomFor(p.Heredocs),
		Comments:    withRoomFor(p.Comments),
		Keywords:    withRoomFor(p.Keywords),
		Tests:       withRoomFor(p.Tests),
	}
}

func withRoomFor[T any](like []T) []T {
	if len(like) == 0 {
		return nil
	}
	return make([]T, 0, len(like)+len(like)/8+8)
}

func (b *builder) visit(node syntax.Node) bool {
	if node == nil {
		top := b.stack[len(b.stack)-1]
		b.stack = b.stack[:len(b.stack)-1]
		if decl, ok := b.parent().(*syntax.FuncDecl); ok && decl.Body == top {
			b.scopes = b.scopes[:len(b.scopes)-1]
		}
		if b.shells[len(b.shells)-1].node == top {
			b.shells = b.shells[:len(b.shells)-1]
		}
		switch {
		case compound(top):
			b.compounds--
		case isNamespace(top):
			b.namespaces = b.namespaces[:len(b.namespaces)-1]
		}
		return true
	}
	b.enter(node)
	b.openShell(node)
	switch n := node.(type) {
	case *syntax.ArrayExpr:
		if compound(n) {
			b.compounds++
		}
	case *syntax.NamespaceClause:
		b.namespaces = append(b.namespaces, n.Name.Value)
	}
	b.stack = append(b.stack, node)
	return true
}

func compound(node syntax.Node) bool {
	array, ok := node.(*syntax.ArrayExpr)
	return ok && len(array.Stmts) > 0
}

func isNamespace(node syntax.Node) bool {
	_, ok := node.(*syntax.NamespaceClause)
	return ok
}

func (b *builder) scope() ScopeID { return b.scopes[len(b.scopes)-1] }

type shellOpening struct {
	node syntax.Node
	id   ShellID
}

func (b *builder) openShell(node syntax.Node) {
	separate, bracketed := b.separateShell(node)
	if !separate || !node.Pos().IsValid() {
		return
	}
	span := b.span(node)
	if bracketed {
		span.Start = min(span.Start+1, span.End)
	}
	id := ShellID(len(b.index.Shells))
	b.index.Shells = append(b.index.Shells, Shell{Span: span, Parent: b.shells[len(b.shells)-1].id})
	b.shells = append(b.shells, shellOpening{node: node, id: id})
}

func (b *builder) separateShell(node syntax.Node) (separate, bracketed bool) {
	switch n := node.(type) {
	case *syntax.Subshell, *syntax.ProcSubst:
		return true, true
	case *syntax.CmdSubst:
		return !n.TempFile && !n.ReplyVar, true
	case *syntax.TestDecl:
		return true, false
	case *syntax.Stmt:
		if n.Background || n.Coprocess || n.Disown {
			return true, false
		}
		switch p := b.parent().(type) {
		case *syntax.CoprocClause:
			return true, false
		case *syntax.BinaryCmd:
			return (p.Op == syntax.Pipe || p.Op == syntax.PipeAll) && (n == p.X || !b.features.LastPipeInShell), false
		}
	}
	return false, false
}

func (b *builder) parent() syntax.Node {
	if len(b.stack) == 0 {
		return nil
	}
	return b.stack[len(b.stack)-1]
}

func (b *builder) enter(node syntax.Node) {
	b.recoveredClosers(node)
	b.keywords(node)
	switch n := node.(type) {
	case *syntax.FuncDecl:
		b.function(n)
	case *syntax.TestDecl:
		b.test(n)
	case *syntax.CallExpr:
		b.call(n)
	case *syntax.DeclClause:
		b.declaration(n)
	case *syntax.ForClause:
		b.forClause(n)
	case *syntax.CoprocClause:
		b.coproc(n)
	case *syntax.ParamExp:
		b.paramExp(n)
		b.namesRead(ZshReads(n))
	case *syntax.FlagsArithm:
		b.namesRead(ZshReads(n))
	case *syntax.RepeatClause:
		b.literalArithmetic(n.Count)
	case *syntax.Word:
		b.arithmeticWord(n)
	case *syntax.BinaryArithm:
		b.arithmeticAssign(n)
	case *syntax.UnaryArithm:
		if n.Op == syntax.Inc || n.Op == syntax.Dec {
			b.arithmeticDefinition(n.X, n)
		}
	case *syntax.Redirect:
		b.redirect(n)
	case *syntax.Comment:
		b.index.Comments = append(b.index.Comments, Comment{Span: b.span(n), Text: n.Text})
	case *syntax.Stmt:
		b.index.Statements++
		liftFunctionList(n)
		b.recoveredStmt(n)
	case *syntax.Lit:
		b.recoveredLit(n)
	case *syntax.BinaryCmd:
		if n.Y != nil && n.Y.Position.IsRecovered() {
			b.errorAt(n.OpPos, fmt.Sprintf("expected a command after %#q", n.Op.String()))
		}
	}
}

func (b *builder) function(n *syntax.FuncDecl) {
	within := b.scope()
	body := ScopeID(len(b.index.Scopes))
	b.index.Scopes = append(b.index.Scopes, Scope{Span: Span{Start: offset(n.Pos()), End: b.span(n.Body).End}, Parent: within})
	names := n.Names
	if n.Name != nil {
		names = []*syntax.Lit{n.Name}
	}
	if b.compounds > 0 {
		names = nil
	}
	for _, name := range names {
		b.index.Functions = append(b.index.Functions, Function{
			Name:      name.Value,
			NameSpan:  b.span(name),
			Span:      b.span(n),
			Body:      body,
			Within:    within,
			Namespace: strings.Join(b.namespaces, "."),
			Doc:       docComment(b.text, offset(n.Pos())),
			Decl:      n,
		})
	}
	b.scopes = append(b.scopes, body)
	for _, param := range n.Params {
		b.index.Definitions = append(b.index.Definitions, Definition{
			Name:     param.Value,
			NameSpan: b.span(param),
			Span:     b.span(param),
			Kind:     DefDeclare,
			Attrs:    AttrNameref,
			Local:    true,
			Within:   body,
			Node:     n,
		})
	}
}

func (b *builder) test(n *syntax.TestDecl) {
	nameSpan := b.span(n.Description)
	test := Test{NameSpan: nameSpan, Span: b.span(n)}
	if name, literal := Literal(n.Description); literal {
		test.Name, test.Segments = name, []string{name}
	} else {
		test.Name, test.Segments = b.text[nameSpan.Start:nameSpan.End], nameSegments(n.Description)
	}
	b.index.Tests = append(b.index.Tests, test)
}

func nameSegments(w *syntax.Word) []string {
	segments := []string{""}
	var run []syntax.WordPart
	flush := func(quoted bool) {
		if len(run) == 0 {
			return
		}
		parts := run
		if quoted {
			parts = []syntax.WordPart{&syntax.DblQuoted{Parts: run}}
		}
		text, _ := Literal(&syntax.Word{Parts: parts})
		segments[len(segments)-1] += text
		run = nil
	}
	var visit func(parts []syntax.WordPart, quoted bool)
	visit = func(parts []syntax.WordPart, quoted bool) {
		for _, part := range parts {
			switch part := part.(type) {
			case *syntax.Lit, *syntax.SglQuoted:
				run = append(run, part)
			case *syntax.DblQuoted:
				flush(quoted)
				visit(part.Parts, true)
				flush(true)
			default:
				flush(quoted)
				segments = append(segments, "")
			}
		}
	}
	visit(w.Parts, false)
	flush(false)
	return segments
}

func (b *builder) call(n *syntax.CallExpr) {
	kind := DefAssign
	if len(n.Args) > 0 {
		kind = DefCommandEnv
	}
	if b.compounds == 0 {
		for _, assign := range n.Assigns {
			b.assign(assign, kind, declFlags{}, false)
		}
	}
	if len(n.Args) == 0 {
		return
	}
	name, ok := Literal(n.Args[0])
	if !ok {
		return
	}
	b.index.Commands = append(b.index.Commands, Command{
		Name:     name,
		NameSpan: b.span(n.Args[0]),
		Span:     b.span(n),
		Args:     n.Args[1:],
		Within:   b.scope(),
	})
	if b.compounds > 0 {
		return
	}
	start := Wrappers{}.Start(n.Args)
	if start < 0 {
		return
	}
	if name, ok = Literal(n.Args[start]); !ok {
		return
	}
	args := n.Args[start+1:]
	if variant, ok := declVariants[name]; ok {
		b.declarationWords(variant, args)
	}
	b.builtinAssignments(n, name, args)
	b.namedOperands(n, name, args)
	if b.features.ArithmeticStatus && ArithmeticStatusBuiltin(name) && len(args) == 1 {
		b.literalArithmetic(args[0])
	}
}

type optionArgument = getopt.Argument[*syntax.Word]

var assigningBuiltins = map[string]map[string]Attr{
	"read":      {"-a": AttrArray},
	"mapfile":   nil,
	"readarray": nil,
	"printf":    {"-v": 0},
	"wait":      {"-p": 0},
}

func (b *builder) builtinAssignments(call *syntax.CallExpr, name string, args []*syntax.Word) {
	if name == "getopts" {
		if len(args) >= 2 {
			b.argumentDefinition(call, optionArgument{Word: args[1]}, 0)
		}
		return
	}
	targets, assigns := assigningBuiltins[name]
	if !assigns {
		return
	}
	parsed := getopt.Parse(getopt.Commands[name], args, Literal)
	for _, opt := range parsed.Options {
		if attrs, assigns := targets[opt.Name]; assigns && opt.Argument != nil {
			b.argumentDefinition(call, *opt.Argument, attrs)
		}
	}
	switch name {
	case "mapfile", "readarray":
		if len(parsed.Operands) > 0 {
			b.argumentDefinition(call, optionArgument{Word: parsed.Operands[0]}, AttrArray)
		}
	case "read":
		for _, w := range parsed.Operands {
			b.argumentDefinition(call, optionArgument{Word: w}, 0)
		}
	}
}

func (b *builder) argumentDefinition(call *syntax.CallExpr, arg optionArgument, attrs Attr) {
	value, ok := arg.Text(Literal)
	if !ok || len(arg.Word.Parts) != 1 {
		return
	}
	if p, ok := parameterAt(value, nameStart(arg.Word)+arg.Offset, attrs); ok {
		b.builtinDefinition(call, p, b.span(arg.Word))
	}
}

func (b *builder) builtinDefinition(call *syntax.CallExpr, p namedParameter, span Span) {
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     p.name,
		NameSpan: p.span,
		Span:     span,
		Kind:     DefBuiltin,
		Attrs:    p.attrs,
		Within:   b.scope(),
		Node:     call,
	})
}

func (b *builder) namedOperands(call *syntax.CallExpr, name string, args []*syntax.Word) {
	parse, ok := dialectNameCommands[b.dialect][name]
	if !ok {
		return
	}
	operands := parse(args)
	for _, p := range operands.reads {
		b.reference(p.name, p.span)
	}
	for _, p := range operands.assigns {
		b.builtinDefinition(call, p, p.span)
	}
	if len(operands.command) == 0 {
		return
	}
	if inner, ok := Literal(operands.command[0]); ok {
		b.namedOperands(call, inner, operands.command[1:])
	}
}

type declVariant struct {
	attrs  Attr
	local  bool
	scoped bool
	types  bool
}

var declVariants = map[string]declVariant{
	"declare":  {scoped: true},
	"typeset":  {scoped: true},
	"local":    {local: true},
	"export":   {attrs: AttrExport},
	"readonly": {attrs: AttrReadonly},
	"nameref":  {attrs: AttrNameref, scoped: true},
	"integer":  {attrs: AttrInteger, scoped: true},
	"float":    {scoped: true},
	"enum":     {types: true},
}

func (b *builder) declaration(n *syntax.DeclClause) {
	variant := declVariants[n.Variant.Value]
	b.index.Commands = append(b.index.Commands, Command{
		Name:     n.Variant.Value,
		NameSpan: b.span(n.Variant),
		Span:     b.span(n),
		Within:   b.scope(),
	})
	if b.compounds > 0 {
		return
	}
	flags := variant.flags()
	for _, arg := range n.Args {
		if arg.Name == nil {
			flags = b.declarationWord(variant, flags, arg.Value)
			continue
		}
		if !flags.namesVariables() {
			continue
		}
		b.assign(arg, DefDeclare, flags, variant.isLocal(b.scope(), flags.global))
	}
}

func (b *builder) declarationWords(variant declVariant, args []*syntax.Word) {
	flags := variant.flags()
	for _, arg := range args {
		flags = b.declarationWord(variant, flags, arg)
	}
}

func (b *builder) declarationWord(variant declVariant, flags declFlags, arg *syntax.Word) declFlags {
	value, literal := Literal(arg)
	if literal && (strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+")) {
		return b.applyFlags(flags, value)
	}
	if !literal {
		lead, ok := arg.Parts[0].(*syntax.Lit)
		if !ok || !strings.Contains(lead.Value, "=") {
			return flags
		}
		value = lead.Value
	}
	name, _, _ := strings.Cut(value, "=")
	if !flags.namesVariables() || !IsName(name) {
		return flags
	}
	start := nameStart(arg)
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     name,
		NameSpan: Span{Start: start, End: start + len(name)},
		Span:     b.span(arg),
		Kind:     DefDeclare,
		Attrs:    flags.attrs,
		Cleared:  flags.cleared,
		Local:    variant.isLocal(b.scope(), flags.global),
		Within:   b.scope(),
		Node:     arg,
	})
	return flags
}

func nameStart(w *syntax.Word) int {
	start := offset(w.Pos())
	switch w.Parts[0].(type) {
	case *syntax.SglQuoted, *syntax.DblQuoted:
		return start + 1
	}
	return start
}

func (v declVariant) flags() declFlags {
	return declFlags{attrs: v.attrs, definesType: v.types}
}

func (v declVariant) isLocal(scope ScopeID, global bool) bool {
	if v.local {
		return true
	}
	return v.scoped && scope != FileScope && !global
}

type declFlags struct {
	attrs       Attr
	cleared     Attr
	global      bool
	inspects    bool
	definesType bool
}

func (f declFlags) namesVariables() bool { return !f.inspects && !f.definesType }

var flagAttrs = map[rune]Attr{
	'a': AttrArray, 'A': AttrAssoc, 'x': AttrExport, 'r': AttrReadonly, 'i': AttrInteger, 'n': AttrNameref,
	'u': AttrTransformed, 'l': AttrTransformed, 'L': AttrTransformed, 'R': AttrTransformed, 'Z': AttrTransformed,
	'U': AttrTransformed, 'E': AttrTransformed, 'F': AttrTransformed,
}

func (b *builder) applyFlags(f declFlags, word string) declFlags {
	set := strings.HasPrefix(word, "-")
	if !set && !strings.HasPrefix(word, "+") {
		return f
	}
	for _, flag := range word[1:] {
		attr, isAttr := flagAttrs[flag]
		switch {
		case flag == 'g':
			f.global = set
		case flag == 'f' || flag == 'p' || flag == 'F' && (b.lang == syntax.LangBash || b.lang == syntax.LangBats):
			f.inspects = set
		case flag == 'T' && b.lang == syntax.LangKsh93:
			f.definesType = set
		case !isAttr:
		case set:
			f.attrs |= attr
			f.cleared &^= attr
		default:
			f.cleared |= attr
			f.attrs &^= attr
		}
	}
	return f
}

func (b *builder) assign(n *syntax.Assign, kind DefinitionKind, flags declFlags, local bool) {
	if n.Name == nil {
		return
	}
	if root, _, member := strings.Cut(n.Name.Value, "."); member && IsName(root) {
		b.referenceStart(n.Name, root)
		return
	}
	if !IsName(n.Name.Value) {
		return
	}
	attrs := flags.attrs
	if n.Array != nil && !compound(n.Array) && !attrs.Has(AttrAssoc) {
		attrs |= AttrArray
	}
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     n.Name.Value,
		NameSpan: b.span(n.Name),
		Span:     b.span(n),
		Kind:     kind,
		Attrs:    attrs,
		Cleared:  flags.cleared,
		Local:    local,
		Within:   b.scope(),
		Value:    assignedWord(n),
		Node:     n,
	})
}

func (b *builder) forClause(n *syntax.ForClause) {
	iter, ok := n.Loop.(*syntax.WordIter)
	if !ok {
		return
	}
	for _, name := range LoopNames(iter) {
		if name == nil || !IsName(name.Value) {
			continue
		}
		b.index.Definitions = append(b.index.Definitions, Definition{
			Name:     name.Value,
			NameSpan: b.span(name),
			Span:     b.span(iter),
			Kind:     DefLoop,
			Within:   b.scope(),
			Node:     iter,
		})
	}
}

func (b *builder) coproc(n *syntax.CoprocClause) {
	name, ok := Literal(n.Name)
	if !ok || !IsName(name) {
		return
	}
	span := b.span(n.Name)
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     name,
		NameSpan: span,
		Span:     Span{Start: offset(n.Pos()), End: span.End},
		Kind:     DefBuiltin,
		Attrs:    AttrArray,
		Within:   b.scope(),
		Node:     n,
	})
}

func (b *builder) paramExp(n *syntax.ParamExp) {
	if n.Param == nil || n.Names != 0 || !n.Param.ValuePos.IsValid() {
		return
	}
	name, _, _ := strings.Cut(n.Param.Value, ".")
	if !IsName(name) {
		return
	}
	b.referenceStart(n.Param, name)
}

func (b *builder) referenceStart(lit *syntax.Lit, name string) {
	start := offset(lit.Pos())
	b.reference(name, Span{Start: start, End: start + len(name)})
}

func (b *builder) reference(name string, span Span) {
	b.index.References = append(b.index.References, Reference{Name: name, Span: span, Within: b.scope()})
}

func (b *builder) namesRead(reads []NameRead) {
	for _, r := range reads {
		b.reference(r.Name, r.Span)
	}
}

func (b *builder) literalArithmetic(w *syntax.Word) {
	reads, _ := LiteralArithmeticReads(w)
	b.namesRead(reads)
}

func (b *builder) arithmeticWord(w *syntax.Word) {
	if name, ok := ArithmeticRead(b.parent(), w); ok {
		b.reference(name, b.span(w))
	}
}

func ArithmeticRead(parent syntax.Node, w *syntax.Word) (string, bool) {
	if !IsArithmetic(parent) {
		return "", false
	}
	if bin, ok := parent.(*syntax.BinaryArithm); ok && bin.Op == syntax.Assgn && bin.X == w {
		return "", false
	}
	return ArithmeticName(w)
}

func IsArithmetic(node syntax.Node) bool {
	switch node := node.(type) {
	case *syntax.BinaryArithm, *syntax.UnaryArithm, *syntax.ParenArithm, *syntax.FuncArithm,
		*syntax.ArithmExp, *syntax.ArithmCmd, *syntax.CStyleLoop, *syntax.LetClause:
		return true
	case *syntax.FlagsArithm:
		return !SubscriptPattern(node)
	}
	return false
}

func ArithmeticName(expr syntax.ArithmExpr) (string, bool) {
	w, ok := expr.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return "", false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok || !IsName(lit.Value) {
		return "", false
	}
	return lit.Value, true
}

func IsArithmeticAssignment(op syntax.BinAritOperator) bool {
	switch op {
	case syntax.Assgn, syntax.AddAssgn, syntax.SubAssgn, syntax.MulAssgn, syntax.QuoAssgn,
		syntax.RemAssgn, syntax.AndAssgn, syntax.OrAssgn, syntax.XorAssgn, syntax.ShlAssgn,
		syntax.ShrAssgn, syntax.AndBoolAssgn, syntax.OrBoolAssgn, syntax.XorBoolAssgn, syntax.PowAssgn:
		return true
	}
	return false
}

func (b *builder) arithmeticAssign(n *syntax.BinaryArithm) {
	if IsArithmeticAssignment(n.Op) {
		b.arithmeticDefinition(n.X, n)
	}
}

func (b *builder) arithmeticDefinition(target syntax.ArithmExpr, expr syntax.ArithmExpr) {
	name, ok := ArithmeticName(target)
	if !ok {
		return
	}
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     name,
		NameSpan: b.span(target),
		Span:     b.span(expr),
		Kind:     DefArithmetic,
		Within:   b.scope(),
		Node:     expr,
	})
}

func (b *builder) descriptorVariable(n *syntax.Redirect) {
	if n.N == nil || len(n.N.Value) < 3 || n.N.Value[0] != '{' || n.N.Value[len(n.N.Value)-1] != '}' {
		return
	}
	name := n.N.Value[1 : len(n.N.Value)-1]
	if !IsName(name) {
		return
	}
	start := offset(n.N.Pos()) + 1
	span := Span{Start: start, End: start + len(name)}
	if target, _ := Literal(n.Word); target == "-" {
		b.reference(name, span)
		return
	}
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     name,
		NameSpan: span,
		Span:     b.span(n),
		Kind:     DefDescriptor,
		Within:   b.scope(),
		Node:     n,
	})
}

func (b *builder) redirect(n *syntax.Redirect) {
	opStart := offset(n.OpPos)
	opSpan := Span{Start: opStart, End: opStart + len(n.Op.String())}
	target := Span{Start: opSpan.End, End: opSpan.End}
	if n.Word != nil && len(n.Word.Parts) > 0 && n.Word.Pos().IsValid() {
		target = b.span(n.Word)
	}
	b.index.Redirects = append(b.index.Redirects, Redirect{
		Op:     n.Op,
		OpSpan: opSpan,
		Span:   Span{Start: offset(n.Pos()), End: target.End},
		Target: target,
	})
	b.descriptorVariable(n)
	if !IsHeredoc(n.Op) {
		return
	}
	delimiter, quoted := HeredocDelimiter(n.Word)
	body := Span{Start: target.End, End: target.End}
	if n.Hdoc != nil && len(n.Hdoc.Parts) > 0 {
		body = b.span(n.Hdoc)
		if !b.closedAtEnd[n] {
			body.End = max(body.Start, textline.Start(b.text, body.End))
		}
	}
	b.index.Heredocs = append(b.index.Heredocs, Heredoc{
		Delimiter: delimiter,
		Quoted:    quoted,
		Strip:     heredocStrip(n.Op),
		Span:      Span{Start: opSpan.Start, End: target.End},
		Body:      body,
		Redirect:  n,
	})
}

func HeredocDelimiter(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	quoted := false
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			quoted = quoted || strings.Contains(p.Value, `\`)
		default:
			quoted = true
		}
	}
	delimiter, _ := Literal(w)
	return delimiter, quoted
}

func (b *builder) recoveredStmt(n *syntax.Stmt) {
	if !n.Position.IsRecovered() {
		return
	}
	switch p := b.parent().(type) {
	case *syntax.BinaryCmd:
		return
	case *syntax.IfClause:
		keyword, name := p.ThenPos, "then"
		switch {
		case b.isElseBranch(p):
			keyword, name = p.Position, "else"
		case p.Braces:
			name = "{"
		}
		b.emptyBody(keyword, p.Position, name)
		return
	case *syntax.WhileClause:
		open, _ := loopDelimiters(p.Braces)
		b.emptyBody(p.DoPos, p.WhilePos, open)
		return
	case *syntax.ForClause:
		open, _ := loopDelimiters(p.Braces)
		b.emptyBody(p.DoPos, p.ForPos, open)
		return
	case *syntax.RepeatClause:
		open, _ := loopDelimiters(p.Braces)
		b.emptyBody(p.DoPos, p.RepeatPos, open)
		return
	case *syntax.Block:
		b.emptyBody(p.Lbrace, p.Lbrace, "{")
		return
	case *syntax.Subshell:
		b.emptyBody(p.Lparen, p.Lparen, "(")
		return
	}
	b.errorAt(b.anchor(), "expected a command")
}

func (b *builder) emptyBody(keyword, fallback syntax.Pos, name string) {
	if keyword.IsValid() {
		b.errorAt(keyword, fmt.Sprintf("expected a command after %#q", name))
		return
	}
	b.errorAt(fallback, "expected a command")
}

func (b *builder) recoveredLit(n *syntax.Lit) {
	if !n.ValuePos.IsRecovered() {
		return
	}
	for _, node := range slices.Backward(b.stack) {
		switch p := node.(type) {
		case *syntax.Redirect:
			b.errorAt(p.OpPos, fmt.Sprintf("expected a word after %#q", p.Op.String()))
			return
		case *syntax.BinaryTest:
			b.errorAt(p.OpPos, fmt.Sprintf("expected a word after %#q", p.Op.String()))
			return
		case *syntax.UnaryTest:
			b.errorAt(p.OpPos, fmt.Sprintf("expected a word after %#q", p.Op.String()))
			return
		}
	}
	b.errorAt(b.anchor(), "expected a word")
}

func (b *builder) anchor() syntax.Pos {
	for _, node := range slices.Backward(b.stack) {
		if pos := node.Pos(); pos.IsValid() {
			return pos
		}
	}
	return syntax.Pos{}
}

func (b *builder) recoveredClosers(node syntax.Node) {
	switch n := node.(type) {
	case *syntax.IfClause:
		opener := "if"
		if b.elseClauses[n] {
			opener = "elif"
		}
		if n.Else != nil {
			b.elseClauses[n.Else] = true
		}
		b.missing(n.Position, n.ThenPos, opener, "then")
		if n.Braces {
			b.missing(n.Position, n.Rbrace, opener, "}")
		}
		if !b.elseClauses[n] {
			b.missing(n.Position, n.FiPos, opener, "fi")
		}
	case *syntax.WhileClause:
		opener := "while"
		if n.Until {
			opener = "until"
		}
		open, closer := loopDelimiters(n.Braces)
		b.missing(n.WhilePos, n.DoPos, opener, open)
		b.missing(n.WhilePos, n.DonePos, opener, closer)
	case *syntax.RepeatClause:
		open, closer := loopDelimiters(n.Braces)
		b.missing(n.RepeatPos, n.DoPos, "repeat", open)
		b.missing(n.RepeatPos, n.DonePos, "repeat", closer)
	case *syntax.ForClause:
		opener := "for"
		switch {
		case n.Select:
			opener = "select"
		case n.Foreach:
			b.missing(n.ForPos, n.DonePos, "foreach", "end")
			return
		}
		open, closer := loopDelimiters(n.Braces)
		if stray := listedDo(n); stray != nil && n.DoPos.IsRecovered() {
			b.errorAt(stray.Pos(), "`do` needs a `;` or line break before it; here it is read as one more word of the list")
		} else {
			b.missing(n.ForPos, n.DoPos, opener, open)
		}
		b.missing(n.ForPos, n.DonePos, opener, closer)
	case *syntax.CaseClause:
		open, closer := "in", "esac"
		if n.Braces {
			open, closer = "{", "}"
		}
		b.missing(n.Case, n.In, "case", open)
		b.missing(n.Case, n.Esac, "case", closer)
	case *syntax.Block:
		b.missing(n.Lbrace, n.Rbrace, "{", "}")
	case *syntax.Subshell:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	case *syntax.CmdSubst:
		opener, closer := "$(", ")"
		switch {
		case n.Backquotes:
			opener, closer = "`", "`"
		case n.TempFile, n.ReplyVar:
			opener, closer = "${", "}"
		}
		b.missing(n.Left, n.Right, opener, closer)
	case *syntax.ArithmExp:
		opener, closer := "$((", "))"
		if n.Bracket {
			opener, closer = "$[", "]"
		}
		b.missing(n.Left, n.Right, opener, closer)
	case *syntax.ArithmCmd:
		b.missing(n.Left, n.Right, "((", "))")
	case *syntax.ParamExp:
		if !n.Short {
			b.missing(n.Dollar, n.Rbrace, "${", "}")
		}
	case *syntax.DblQuoted:
		b.missing(n.Left, n.Right, `"`, `"`)
	case *syntax.SglQuoted:
		b.missing(n.Left, n.Right, "'", "'")
	case *syntax.TestClause:
		b.missing(n.Left, n.Right, "[[", "]]")
	case *syntax.ProcSubst:
		b.missing(n.OpPos, n.Rparen, n.Op.String(), ")")
	case *syntax.ArrayExpr:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	case *syntax.ParenTest:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	case *syntax.ParenArithm:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	}
}

func listedDo(n *syntax.ForClause) *syntax.Word {
	iter, ok := n.Loop.(*syntax.WordIter)
	if !ok || len(iter.Items) == 0 {
		return nil
	}
	if last := iter.Items[len(iter.Items)-1]; last.Lit() == "do" {
		return last
	}
	return nil
}

func (b *builder) missing(anchor, closer syntax.Pos, opener, token string) {
	if closer.IsRecovered() {
		b.errorAt(anchor, fmt.Sprintf("missing %#q to match %#q", token, opener))
	}
}

func (b *builder) errorAt(pos syntax.Pos, message string) {
	if !pos.IsValid() {
		return
	}
	b.errs = append(b.errs, Error{Span: tokenSpan(b.text, offset(pos)), Kind: ErrSyntax, Message: message})
}

func (b *builder) span(n syntax.Node) Span {
	start := offset(n.Pos())
	end := n.End()
	if !end.IsValid() {
		return Span{Start: start, End: len(b.text)}
	}
	return Span{Start: start, End: max(start, offset(end))}
}

func IsDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func IsNameChar(c byte) bool {
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

func IsName(s string) bool {
	if s == "" || ('0' <= s[0] && s[0] <= '9') {
		return false
	}
	for i := range len(s) {
		if !IsNameChar(s[i]) {
			return false
		}
	}
	return true
}

func docComment(text string, start int) string {
	lineStart := textline.Start(text, start)
	var lines []string
	for line := range strings.Lines(text[textline.CommentBlockStart(text, lineStart):lineStart]) {
		body := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(line), "#"), " ")
		if !strings.HasPrefix(body, "shellcheck ") {
			lines = append(lines, body)
		}
	}
	return strings.Join(lines, "\n")
}

func (b *builder) isElseBranch(n *syntax.IfClause) bool {
	return b.elseClauses[n] && IsElse(n)
}

func (b *builder) keywords(node syntax.Node) {
	switch n := node.(type) {
	case *syntax.IfClause:
		switch {
		case b.isElseBranch(n):
			b.keyword("else", n.Position)
		case b.elseClauses[n]:
			b.keyword("elif", n.Position)
		default:
			b.keyword("if", n.Position)
			b.keyword("fi", n.FiPos)
		}
		if !n.Braces {
			b.keyword("then", n.ThenPos)
		}
	case *syntax.WhileClause:
		name := "while"
		if n.Until {
			name = "until"
		}
		b.keyword(name, n.WhilePos)
		b.loopKeywords(n.Braces, n.DoPos, n.DonePos)
	case *syntax.TryClause:
		b.keyword("always", n.AlwaysPos)
	case *syntax.NamespaceClause:
		b.keyword("namespace", n.Namespace)
	case *syntax.RepeatClause:
		b.keyword("repeat", n.RepeatPos)
		b.loopKeywords(n.Braces, n.DoPos, n.DonePos)
	case *syntax.ForClause:
		name := "for"
		switch {
		case n.Select:
			name = "select"
		case n.Foreach:
			name = "foreach"
		}
		b.keyword(name, n.ForPos)
		if iter, ok := n.Loop.(*syntax.WordIter); ok {
			b.keyword("in", iter.InPos)
		}
		if n.Foreach {
			b.keyword("end", n.DonePos)
		} else {
			b.loopKeywords(n.Braces, n.DoPos, n.DonePos)
		}
	case *syntax.CaseClause:
		b.keyword("case", n.Case)
		if !n.Braces {
			b.keyword("in", n.In)
			b.keyword("esac", n.Esac)
		}
	case *syntax.FuncDecl:
		if n.RsrvWord {
			b.keyword("function", n.Position)
		}
	case *syntax.TestClause:
		b.keyword("[[", n.Left)
		b.keyword("]]", n.Right)
	case *syntax.TimeClause:
		b.keyword("time", n.Time)
	case *syntax.CoprocClause:
		b.keyword("coproc", n.Coproc)
	case *syntax.Stmt:
		if n.Negated && n.Position.IsValid() {
			b.keyword("!", n.Position)
		}
	}
}

func (b *builder) loopKeywords(braces bool, doPos, donePos syntax.Pos) {
	if braces {
		return
	}
	b.keyword("do", doPos)
	b.keyword("done", donePos)
}

func (b *builder) keyword(name string, pos syntax.Pos) {
	if !pos.IsValid() {
		return
	}
	start := offset(pos)
	b.index.Keywords = append(b.index.Keywords, Keyword{Name: name, Span: Span{Start: start, End: start + len(name)}})
}

func assignedWord(n *syntax.Assign) *syntax.Word {
	if n.Naked || n.Append || n.Index != nil || n.Value == nil || len(n.Value.Parts) == 0 {
		return nil
	}
	return n.Value
}

func (ix *Index) buildLookups() {
	ix.definitionsByBinding = make(map[binding][]int, len(ix.Definitions))
	ix.attrsByBinding = make(map[binding]Attr, len(ix.Definitions))
	ix.attrsByName = make(map[string]Attr, len(ix.Definitions))
	ix.definitionsByScope = make(map[ScopeID][]int, len(ix.Scopes))
	ix.localScopes = map[string]map[ScopeID]bool{}
	for _, def := range ix.Definitions {
		if !def.Local {
			continue
		}
		if ix.localScopes[def.Name] == nil {
			ix.localScopes[def.Name] = map[ScopeID]bool{}
		}
		ix.localScopes[def.Name][def.Within] = true
	}
	for i, def := range ix.Definitions {
		key := ix.visibleBinding(def.Name, def.Within)
		ix.definitionsByBinding[key] = append(ix.definitionsByBinding[key], i)
		ix.attrsByBinding[key] |= def.Attrs
		ix.attrsByName[def.Name] |= def.Attrs
		ix.definitionsByScope[def.Within] = append(ix.definitionsByScope[def.Within], i)
	}
	ix.functionsByName = make(map[string]int, len(ix.Functions))
	ix.functionsByBody = make(map[ScopeID]int, len(ix.Functions))
	for i, fn := range ix.Functions {
		if _, seen := ix.functionsByName[fn.Name]; !seen {
			ix.functionsByName[fn.Name] = i
		}
		if _, seen := ix.functionsByBody[fn.Body]; !seen {
			ix.functionsByBody[fn.Body] = i
		}
	}
}
