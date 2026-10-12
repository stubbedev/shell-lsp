// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"fmt"
	"io"
	"iter"
	"math/bits"
	"slices"
	"strings"
	"unicode/utf8"
)

// ParserOption is a function which can be passed to NewParser
// to alter its behavior. To apply option to existing Parser
// call it directly, for example KeepComments(true)(parser).
type ParserOption func(*Parser)

// KeepComments makes the parser parse comments and attach them to
// nodes, as opposed to discarding them.
func KeepComments(enabled bool) ParserOption {
	return func(p *Parser) { p.keepComments = enabled }
}

// LangVariant describes a shell language variant to use when tokenizing and
// parsing shell code. The zero value is [LangBash].
//
// This type implements [flag.Value] so that it can be used as a CLI flag.
type LangVariant int

// TODO(v4): the zero value should be left as an unset and invalid value.
// TODO(v4): the type should be uint32 now that we use this as a bitset;
// an unsigned integer is clearer, and being agnostic to uint size avoids issues.

const (
	// LangBash corresponds to the GNU Bash language, as described in its
	// manual at https://www.gnu.org/software/bash/manual/bash.html.
	//
	// We currently follow Bash version 5.2.
	//
	// Its string representation is "bash".
	LangBash LangVariant = 1 << iota

	// LangPOSIX corresponds to the POSIX Shell language, as described at
	// https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html.
	//
	// Its string representation is "posix" or "sh".
	LangPOSIX

	// LangMirBSDKorn corresponds to the MirBSD Korn Shell, also known as
	// mksh, as described at http://www.mirbsd.org/htman/i386/man1/mksh.htm.
	// Note that it shares some features with Bash, due to the shared
	// ancestry that is ksh.
	//
	// We currently follow mksh version 59.
	//
	// Its string representation is "mksh".
	LangMirBSDKorn

	// LangBats corresponds to the Bash Automated Testing System language,
	// as described at https://github.com/bats-core/bats-core. Note that
	// it's just a small extension of the Bash language.
	//
	// Its string representation is "bats".
	LangBats

	// LangZsh corresponds to the Z shell, as described at https://www.zsh.org/.
	//
	// Note that its support in the syntax package is experimental and
	// incomplete for now. See https://github.com/mvdan/sh/issues/120.
	//
	// We currently follow Zsh version 5.9.
	//
	// Its string representation is "zsh".
	LangZsh

	// LangKsh93 corresponds to the AT&T Korn Shell 93, as described in its
	// manual at https://github.com/ksh93/ksh/blob/dev/src/cmd/ksh93/sh.1.
	// It shares most of its features with [LangMirBSDKorn], given their
	// shared ancestry, and adds its own, such as compound variables.
	//
	// We currently follow ksh 93u+m.
	//
	// Its string representation is "ksh93".
	LangKsh93

	// langResolvedVariantsCount is the number of variants declared above,
	// that is, langResolvedVariants.count() as a constant.
	langResolvedVariantsCount = iota

	// LangAuto corresponds to automatic language detection,
	// commonly used by end-user applications like shfmt,
	// which can guess a file's language variant given its filename or shebang.
	//
	// At this time, [Variant] does not support LangAuto.
	LangAuto LangVariant = 1 << langResolvedVariantsCount

	// langBashLegacy is what [LangBash] used to be, when it was zero.
	// We still support it for the sake of backwards compatibility.
	langBashLegacy LangVariant = 0

	// langResolvedVariants contains all known variants except [LangAuto],
	// which is meant to resolve to another variant.
	langResolvedVariants = LangBash | LangPOSIX | LangMirBSDKorn | LangBats | LangZsh | LangKsh93

	// langKshLike contains the Korn shell variants.
	langKshLike = LangMirBSDKorn | LangKsh93

	// langBashLike contains Bash plus all variants which are extensions of it.
	langBashLike = LangBash | LangBats

	// langLazyParamExp contains the variants whose shells only check what
	// follows ${ when the expansion runs, so that a script with an expansion
	// like ${.sh.file} or ${(f)x} parses, and fails only if it runs it.
	langLazyParamExp = langBashLike | LangPOSIX | LangMirBSDKorn
)

// Variant changes the shell language variant that the parser will
// accept.
//
// The passed language variant must be one of the constant values defined in
// this package.
func Variant(l LangVariant) ParserOption {
	switch l {
	case langBashLegacy:
		l = LangBash
	case LangBash, LangPOSIX, LangMirBSDKorn, LangBats, LangZsh, LangKsh93:
	case LangAuto:
		panic("LangAuto is not supported by the parser at this time")
	default:
		panic(fmt.Sprintf("unknown shell language variant: %#b", l))
	}
	return func(p *Parser) { p.lang = l }
}

func (l LangVariant) String() string {
	switch l {
	case langBashLegacy, LangBash:
		return "bash"
	case LangPOSIX:
		return "posix"
	case LangMirBSDKorn:
		return "mksh"
	case LangBats:
		return "bats"
	case LangZsh:
		return "zsh"
	case LangKsh93:
		return "ksh93"
	case LangAuto:
		return "auto"
	}
	return "unknown shell language variant"
}

func (l *LangVariant) Set(s string) error {
	switch s {
	case "bash":
		*l = LangBash
	case "posix", "sh", "dash":
		*l = LangPOSIX
	case "mksh":
		*l = LangMirBSDKorn
	case "bats":
		*l = LangBats
	case "zsh":
		*l = LangZsh
	case "ksh", "ksh93":
		*l = LangKsh93
	case "auto":
		*l = LangAuto
	default:
		return fmt.Errorf("unknown shell language variant: %q", s)
	}
	return nil
}

func (l LangVariant) in(l2 LangVariant) bool {
	return l&l2 == l
}

func (l LangVariant) count() int {
	return bits.OnesCount32(uint32(l))
}

func (l LangVariant) index() int {
	return bits.TrailingZeros32(uint32(l))
}

func (l LangVariant) bits() iter.Seq[LangVariant] {
	return func(yield func(LangVariant) bool) {
		for n := LangVariant(1); n < langResolvedVariants; n <<= 1 {
			if l&n == 0 {
				continue
			}
			if !yield(n) {
				return
			}
		}
	}
}

// StopAt configures the lexer to stop at an arbitrary word, treating it
// as if it were the end of the input. It can contain any characters
// except whitespace, and cannot be over four bytes in size.
//
// This can be useful to embed shell code within another language, as
// one can use a special word to mark the delimiters between the two.
//
// As a word, it will only apply when following whitespace or a
// separating token. For example, StopAt("$$") will act on the inputs
// "foo $$" and "foo;$$", but not on "foo '$$'".
//
// The match is done by prefix, so the example above will also act on
// "foo $$bar".
func StopAt(word string) ParserOption {
	if len(word) > 4 {
		panic("stop word can't be over four bytes in size")
	}
	if strings.ContainsAny(word, " \t\n\r") {
		panic("stop word can't contain whitespace characters")
	}
	return func(p *Parser) { p.stopAt = []byte(word) }
}

// RecoverErrors allows the parser to skip up to a maximum number of
// errors in the given input on a best-effort basis.
// This can be useful to tab-complete an interactive shell prompt,
// or when providing diagnostics on slightly incomplete shell source.
//
// Currently, this only helps with mandatory tokens from the shell grammar
// which are not present in the input. They result in position fields
// or nodes whose position report [Pos.IsRecovered] as true.
//
// For example, given the input
//
//	(foo |
//
// the result will contain two recovered positions; first, the pipe requires
// a statement to follow, and as [Stmt.Pos] reports, the entire node is recovered.
// Second, the subshell needs to be closed, so [Subshell.Rparen] is recovered.
func RecoverErrors(maximum int) ParserOption {
	return func(p *Parser) { p.recoverErrorsMax = maximum }
}

// StartAt makes the positions of the parsed nodes start at the given
// offset, line and column instead of at the beginning of the input,
// so that a fragment of a larger file can be parsed on its own and
// still report the positions it has in that file.
//
// Lines and columns are 1-based, like those of [Pos].
func StartAt(offset, line, col uint) ParserOption {
	return func(p *Parser) {
		p.startOffs, p.startLine, p.startCol = int64(offset), int64(line), int64(col)
	}
}

// NewParser allocates a new [Parser] and applies any number of options.
func NewParser(options ...ParserOption) *Parser {
	p := &Parser{
		lang:      LangBash,
		startLine: 1,
		startCol:  1,
	}
	for _, opt := range options {
		opt(p)
	}
	return p
}

// Parse reads and parses a shell program with an optional name. It
// returns the parsed program if no issues were encountered. Otherwise,
// an error is returned. Reads from r are buffered.
//
// Parse can be called more than once, but not concurrently. That is, a
// Parser can be reused once it is done working.
func (p *Parser) Parse(r io.Reader, name string) (*File, error) {
	p.reset()
	p.f = &File{Name: name}
	p.src = r
	p.rune()
	p.next()
	p.f.Stmts, p.f.Last = p.stmtList()
	if p.err == nil {
		// EOF immediately after heredoc word so no newline to
		// trigger the parsing error.
		p.doHeredocs()
	}
	return p.f, p.err
}

// Stmts is a pre-iterators API which now wraps [Parser.StmtsSeq].
//
// Deprecated: use [Parser.StmtsSeq].
func (p *Parser) Stmts(r io.Reader, fn func(*Stmt) bool) error {
	for stmt, err := range p.StmtsSeq(r) {
		if err != nil {
			return err
		}
		if !fn(stmt) {
			break
		}
	}
	return nil
}

// StmtsSeq reads and parses statements one at a time via an iterator.
func (p *Parser) StmtsSeq(r io.Reader) iter.Seq2[*Stmt, error] {
	p.reset()
	p.f = &File{}
	p.src = r
	return func(yield func(*Stmt, error) bool) {
		p.rune()
		p.next()
		p.stmts(yield, false)
		if p.err == nil {
			// EOF immediately after heredoc word so no newline to
			// trigger the parsing error.
			p.doHeredocs()
		}
		if p.err != nil {
			// Yield any final error from the parser.
			yield(nil, p.err)
		}
	}
}

type wrappedReader struct {
	p  *Parser
	rd io.Reader

	lastLine    int64
	accumulated []*Stmt
	yield       func([]*Stmt, error) bool
}

func (w *wrappedReader) Read(p []byte) (n int, err error) {
	// If we lexed a newline for the first time, we just finished a line, so
	// we may need to give a callback for the edge cases below not covered
	// by [Parser.Stmts].
	if (w.p.r == '\n' || w.p.r == escNewl) && w.p.line > w.lastLine {
		if w.p.Incomplete() {
			// Incomplete statement; call back to print "> ".
			if !w.yield(w.accumulated, w.p.err) {
				return 0, io.EOF
			}
		} else if len(w.accumulated) == 0 {
			// Nothing was parsed; call back to print another "$ ".
			if !w.yield(nil, w.p.err) {
				return 0, io.EOF
			}
		}
		w.lastLine = w.p.line
	}
	return w.rd.Read(p)
}

// Interactive is a pre-iterators API which now wraps [Parser.InteractiveSeq].
//
// Deprecated: use [Parser.InteractiveSeq].
func (p *Parser) Interactive(r io.Reader, fn func([]*Stmt) bool) error {
	for stmts, err := range p.InteractiveSeq(r) {
		if err != nil {
			return err
		}
		if !fn(stmts) {
			break
		}
	}
	return nil
}

// InteractiveSeq implements what is necessary to parse statements in an
// interactive shell. The parser will call the given function under two
// circumstances outlined below.
//
// If a line containing any number of statements is parsed, the function will be
// called with said statements.
//
// If a line ending in an incomplete statement is parsed, the function will be
// called with any fully parsed statements, and [Parser.Incomplete] will return true.
//
// One can imagine a simple interactive shell implementation as follows:
//
//	fmt.Fprintf(os.Stdout, "$ ")
//	parser.Interactive(os.Stdin, func(stmts []*syntax.Stmt) bool {
//		if parser.Incomplete() {
//			fmt.Fprintf(os.Stdout, "> ")
//			return true
//		}
//		run(stmts)
//		fmt.Fprintf(os.Stdout, "$ ")
//		return true
//	}
//
// If the callback function returns false, parsing is stopped and the function
// is not called again.
func (p *Parser) InteractiveSeq(r io.Reader) iter.Seq2[[]*Stmt, error] {
	return func(yield func([]*Stmt, error) bool) {
		w := wrappedReader{p: p, rd: r, yield: yield}
		for stmts, err := range p.StmtsSeq(&w) {
			w.accumulated = append(w.accumulated, stmts)
			if err != nil {
				if !yield(w.accumulated, err) {
					break
				}
				// If the caller wishes, they can continue in the presence of parse errors.
				// TODO: does this even work? Write tests for it. This only came up
				continue
			}
			// We finished parsing a statement and we're at a newline token,
			// so we finished fully parsing a number of statements. Call
			// back to run the statements and print "$ ".
			if p.tok == _Newl {
				if !yield(w.accumulated, nil) {
					break
				}
				w.accumulated = w.accumulated[:0]
				// The callback above would already print "$ ", so we
				// don't want the subsequent wrappedReader.Read to cause
				// another "$ " print thinking that nothing was parsed.
				w.lastLine = w.p.line + 1
			}
		}
	}
}

// Words is a pre-iterators API which now wraps [Parser.WordsSeq].
//
// Deprecated: use [Parser.WordsSeq].
func (p *Parser) Words(r io.Reader, fn func(*Word) bool) error {
	for w, err := range p.WordsSeq(r) {
		if err != nil {
			return err
		}
		if !fn(w) {
			break
		}
	}
	return nil
}

// WordsSeq reads and parses a sequence of words alongside any error encountered.
//
// Newlines are skipped, meaning that multi-line input will work fine. If the
// parser encounters a token that isn't a word, such as a semicolon, an error
// will be returned.
//
// Note that the lexer doesn't currently tokenize spaces, so it may need to read
// a non-space byte such as a newline or a letter before finishing the parsing
// of a word. This will be fixed in the future.
func (p *Parser) WordsSeq(r io.Reader) iter.Seq2[*Word, error] {
	p.reset()
	p.f = &File{}
	p.src = r
	return func(yield func(*Word, error) bool) {
		p.rune()
		p.next()
		for {
			p.got(_Newl)
			w := p.getWord()
			if w == nil {
				if p.tok != _EOF {
					p.curErr("%#q is not a valid word", p.tok)
				}
				if p.err != nil {
					yield(nil, p.err)
				}
				return
			}
			if !yield(w, nil) {
				return
			}
		}
	}
}

// Document parses a single here-document word. That is, it parses the input as
// if they were lines following a <<EOF redirection.
//
// In practice, this is the same as parsing the input as if it were within
// double quotes, but without having to escape all double quote characters.
// Similarly, the here-document word parsed here cannot be ended by any
// delimiter other than reaching the end of the input.
func (p *Parser) Document(r io.Reader) (*Word, error) {
	p.reset()
	p.f = &File{}
	p.src = r
	p.rune()
	p.quote = hdocBody
	p.hdocStops = [][]byte{[]byte("MVDAN_CC_SH_SYNTAX_EOF")}
	p.parsingDoc = true
	p.next()
	w := p.getWord()
	return w, p.err
}

// Arithmetic parses a single arithmetic expression. That is, as if the input
// were within the $(( and )) tokens.
func (p *Parser) Arithmetic(r io.Reader) (ArithmExpr, error) {
	p.reset()
	p.f = &File{}
	p.src = r
	p.rune()
	p.quote = arithmExpr
	p.next()
	expr := p.arithmExpr(false)
	return expr, p.err
}

// Parser holds the internal state of the parsing mechanism of a
// program.
type Parser struct {
	src io.Reader
	bs  []byte // current chunk of read bytes
	bsp uint   // offset within [Parser.bs] for the rune after [Parser.r]
	r   rune   // next rune; [runeEOF] when it went past EOF, or we stopped
	w   int    // width of [Parser.r]

	f *File

	spaced bool // whether [Parser.tok] has whitespace on its left

	err     error // lexer/parser error
	readErr error // got a read error, but bytes left
	readEOF bool  // [Parser.src] already gave us an [io.EOF] error

	tok token  // current token
	val string // current value (valid if tok is _Lit*)

	// position of [Parser.r], to be converted to [Parser.pos] later
	offs, line, col int64

	// position of the first rune, as set by [StartAt]
	startOffs, startLine, startCol int64

	pos Pos // position of tok

	quote   quoteState // current lexer state
	eqlOffs int        // position of '=' in [Parser.val] when [Parser.tok].isLit is true

	keepComments bool
	lang         LangVariant

	stopAt []byte

	recoveredErrors  int
	recoverErrorsMax int

	forbidNested bool

	// wordCont is set while the parts of a word are being parsed, so that
	// a '#' right after one of them continues the word instead of starting
	// a comment, also inside command substitutions.
	wordCont bool

	// list of pending heredoc bodies
	buriedHdocs int
	heredocs    []*Redirect

	hdocStops [][]byte // stack of end words for open heredocs

	parsingDoc bool // true if using [Parser.Document]

	// nesting is how deeply the node being parsed sits in the syntax tree.
	nesting int

	// openNodes tracks how many entire statements or words we're currently parsing.
	// A non-zero number means that we require certain tokens or words before
	// reaching EOF, used for [Parser.Incomplete].
	openNodes int
	// openBquotes is how many levels of backquotes are open at the moment.
	openBquotes int
	// openBquoteDbls is how many of those backquote levels began inside
	// double quotes, where backslashes also escape double quotes.
	openBquoteDbls int

	// lastBquoteEsc is how many times the last backquote token was escaped
	lastBquoteEsc int

	rxOpenParens int
	rxFirstPart  bool

	// subFlagsBrackets is how many brackets are open in the current
	// [paramExpSubFlagsArg] or [paramExpKey] word.
	subFlagsBrackets int

	// kshTypes holds the names of the types defined so far with enum or
	// typeset -T, whose commands are declarations with [LangKsh93].
	kshTypes map[string]bool

	// assocArrays holds the names declared so far as associative arrays,
	// like with `declare -A name`, whose subscripts are string keys.
	assocArrays map[string]bool

	accComs []Comment
	curComs *[]Comment

	litBatch  []Lit
	wordBatch []wordAlloc

	readBuf [bufSize]byte
	litBuf  [bufSize]byte
	litBs   []byte
}

// Incomplete reports whether the parser needs more input bytes
// to finish properly parsing a statement or word.
//
// It is only safe to call while the parser is blocked on a read. For an example
// use case, see [Parser.Interactive].
func (p *Parser) Incomplete() bool {
	// If there are any open nodes, we need to finish them.
	// If we're constructing a literal, we need to finish it.
	return p.openNodes > 0 || len(p.litBs) > 0
}

const bufSize = 1 << 10

func (p *Parser) reset() {
	p.tok, p.val = illegalTok, ""
	p.eqlOffs = 0
	p.bs, p.bsp = nil, 0
	p.offs, p.line, p.col = p.startOffs, p.startLine, p.startCol
	p.r, p.w = 0, 0
	p.err, p.readErr, p.readEOF = nil, nil, false
	p.quote, p.forbidNested = noState, false
	p.openNodes = 0
	p.nesting = 0
	p.recoveredErrors = 0
	p.heredocs, p.buriedHdocs = p.heredocs[:0], 0
	p.hdocStops = nil
	p.parsingDoc = false
	p.openBquotes = 0
	p.openBquoteDbls = 0
	p.subFlagsBrackets = 0
	p.accComs = nil
	p.accComs, p.curComs = nil, &p.accComs
	p.litBatch = nil
	p.wordBatch = nil
	p.litBs = nil
	clear(p.kshTypes)
	clear(p.assocArrays)
}

// nextPos returns the position of the next rune, [Parser.r].
func (p *Parser) nextPos() Pos {
	// Basic protection against offset overflow;
	// note that an offset of 0 is valid, so we leave the maximum.
	offset := min(p.offs+int64(p.bsp)-int64(p.w), offsetMax)
	var line, col uint
	if p.line <= lineMax {
		line = uint(p.line)
	}
	if p.col <= colMax {
		col = uint(p.col)
	}
	return NewPos(uint(offset), line, col)
}

func (p *Parser) lit(pos Pos, val string) *Lit {
	if len(p.litBatch) == 0 {
		p.litBatch = make([]Lit, 32)
	}
	l := &p.litBatch[0]
	p.litBatch = p.litBatch[1:]
	l.ValuePos = pos
	l.ValueEnd = p.nextPos()
	l.Value = val
	return l
}

type wordAlloc struct {
	word  Word
	parts [1]WordPart
}

func (p *Parser) wordAnyNumber() *Word {
	if len(p.wordBatch) == 0 {
		p.wordBatch = make([]wordAlloc, 32)
	}
	alloc := &p.wordBatch[0]
	p.wordBatch = p.wordBatch[1:]
	w := &alloc.word
	w.Parts = p.wordParts(alloc.parts[:0])
	return w
}

func (p *Parser) wordOne(part WordPart) *Word {
	if len(p.wordBatch) == 0 {
		p.wordBatch = make([]wordAlloc, 32)
	}
	alloc := &p.wordBatch[0]
	p.wordBatch = p.wordBatch[1:]
	w := &alloc.word
	w.Parts = alloc.parts[:1]
	w.Parts[0] = part
	return w
}

func (p *Parser) call(w *Word) *CallExpr {
	var alloc struct {
		ce CallExpr
		ws [4]*Word
	}
	ce := &alloc.ce
	ce.Args = alloc.ws[:1]
	ce.Args[0] = w
	return ce
}

type quoteState uint32

const (
	// The initial state of the parser.
	noState quoteState = 1 << iota

	// Used when parsing parameter expansions; use with [Parser.rune],
	// [Parser.next] always returns [illegalTok].
	runeByRune

	// unquotedWordCont exists purely so that the '#' in $foo#bar does not
	// get parsed as a comment; it's a tiny variation on [noState].
	unquotedWordCont

	subCmd
	subCmdBckquo
	// subCmdBraces is like subCmd, but for `${ stmts;}` and `${|stmts;}`,
	// whose bodies end at a word beginning with `}`.
	subCmdBraces
	dblQuotes
	hdocWord
	hdocBody
	hdocBodyTabs
	// hdocBodyBlanks is like hdocBodyTabs, but for ksh93's <<# here-documents,
	// whose lines may be indented with spaces too.
	hdocBodyBlanks
	arithmExpr
	arithmExprLet
	arithmExprCmd
	testExpr
	testExprRegexp
	switchCase
	// paramExpArithm is a subscript like ${a[i]}, which can be a string key
	// rather than an arithmetic expression when the array is associative.
	paramExpArithm
	// paramExpSlice is a slice like ${a:i:j}, which is always arithmetic.
	paramExpSlice
	paramExpRepl
	paramExpExp
	// paramExpSubFlagsArg is the argument after zsh subscript flags, such as
	// the pattern in ${a[(r)pattern]}, which ends at a ',' or ']' outside of
	// any brackets within it.
	paramExpSubFlagsArg
	// paramExpKey is the subscript of an associative array, like the `e/`
	// in ${tree[e/]}, which is a string key ending at a ']' outside of any
	// brackets within it.
	paramExpKey
	arrayElems

	allKeepSpaces = runeByRune | paramExpRepl | dblQuotes | hdocBody |
		hdocBodyTabs | hdocBodyBlanks | paramExpRepl | paramExpExp |
		paramExpSubFlagsArg | paramExpKey
	allRegTokens = noState | unquotedWordCont | subCmd | subCmdBckquo | subCmdBraces |
		hdocWord | switchCase | arrayElems | testExpr
	allArithmExpr = arithmExpr | arithmExprLet | arithmExprCmd |
		paramExpArithm | paramExpSlice
	allParamExp = paramExpArithm | paramExpSlice | paramExpRepl | paramExpExp
)

type saveState struct {
	quote       quoteState
	buriedHdocs int
	wordCont    bool
}

// maxNesting is how deeply the syntax tree may nest. Deeper input, such as
// thousands of nested command substitutions or a list of thousands of
// commands joined by "&&", is rejected with an error, so that neither
// parsing it nor walking the resulting tree recursively can exhaust the
// stack.
const maxNesting = 1000

// nest records that the node being parsed sits one level deeper in the
// syntax tree, and reports an error past [maxNesting]. Each call is undone
// by [Parser.unnest].
func (p *Parser) nest() {
	if p.nesting++; p.nesting > maxNesting {
		p.curErr("syntax nests more than %d levels deep", maxNesting)
	}
}

func (p *Parser) unnest(levels int) {
	p.nesting -= levels
}

func (p *Parser) preNested(quote quoteState) (s saveState) {
	s.quote, s.buriedHdocs, s.wordCont = p.quote, p.buriedHdocs, p.wordCont
	p.buriedHdocs, p.quote, p.wordCont = len(p.heredocs), quote, false
	return s
}

func (p *Parser) postNested(s saveState) {
	p.quote, p.buriedHdocs, p.wordCont = s.quote, s.buriedHdocs, s.wordCont
}

func (p *Parser) unquotedWordBytes(w *Word) ([]byte, bool) {
	buf := make([]byte, 0, 4)
	didUnquote := false
	for _, wp := range w.Parts {
		buf, didUnquote = p.unquotedWordPart(buf, wp, false)
	}
	return buf, didUnquote
}

func (p *Parser) unquotedWordPart(buf []byte, wp WordPart, quotes bool) (_ []byte, quoted bool) {
	switch wp := wp.(type) {
	case *Lit:
		for i := 0; i < len(wp.Value); i++ {
			if b := wp.Value[i]; b == '\\' && !quotes {
				if i++; i < len(wp.Value) {
					buf = append(buf, wp.Value[i])
				}
				quoted = true
			} else {
				buf = append(buf, b)
			}
		}
	case *SglQuoted:
		buf = append(buf, []byte(wp.Value)...)
		quoted = true
	case *DblQuoted:
		for _, wp2 := range wp.Parts {
			buf, _ = p.unquotedWordPart(buf, wp2, true)
		}
		quoted = true
	}
	return buf, quoted
}

func (p *Parser) doHeredocs() {
	hdocs := p.heredocs[p.buriedHdocs:]
	if len(hdocs) == 0 {
		// Nothing do do; don't even issue a read.
		return
	}
	p.rune() // consume '\n', since we know p.tok == _Newl
	old := p.quote
	p.heredocs = p.heredocs[:p.buriedHdocs]
	for i, r := range hdocs {
		if p.err != nil {
			break
		}
		p.quote = hdocBody
		switch r.Op {
		case DashHdoc:
			p.quote = hdocBodyTabs
		case HashHdoc:
			p.quote = hdocBodyBlanks
		}
		stop, quoted := p.unquotedWordBytes(r.Word)
		p.hdocStops = append(p.hdocStops, stop)
		if i > 0 && p.r == '\n' {
			p.rune()
		}
		if quoted {
			r.Hdoc = p.quotedHdocWord()
		} else {
			p.next()
			r.Hdoc = p.getWord()
		}
		if stop := p.hdocStops[len(p.hdocStops)-1]; stop != nil {
			p.posErr(r.Pos(), "unclosed here-document %#q", stop)
		}
		p.hdocStops = p.hdocStops[:len(p.hdocStops)-1]
	}
	p.quote = old
}

func (p *Parser) got(tok token) bool {
	if p.tok == tok {
		p.next()
		return true
	}
	return false
}

func (p *Parser) gotRsrv(val string) (Pos, bool) {
	pos := p.pos
	if p.tok == _LitWord && p.val == val {
		p.next()
		return pos, true
	}
	return pos, false
}

func (p *Parser) recoverError() bool {
	if p.recoveredErrors < p.recoverErrorsMax {
		p.recoveredErrors++
		return true
	}
	return false
}

type noQuote string

func (s noQuote) Format(f fmt.State, verb rune) {
	f.Write([]byte(s))
}

func (t token) Format(f fmt.State, verb rune) {
	if t < _realTokenBoundary && verb == 'q' {
		// EOF, Lit and the others should not be quoted in error messages
		// as they are not real shell syntax like `if` or `{`.
		f.Write([]byte(t.String()))
	} else {
		fmt.Fprintf(f, fmt.FormatString(f, verb), t.String())
	}
}

func (p *Parser) followErr(pos Pos, left, right any) {
	p.posErr(pos, "%#q must be followed by %#q", left, right)
}

func (p *Parser) followErrExp(pos Pos, left any) {
	p.followErr(pos, left, noQuote("an expression"))
}

func (p *Parser) follow(lpos Pos, left string, tok token) {
	if !p.got(tok) {
		p.followErr(lpos, left, tok)
	}
}

func (p *Parser) followRsrv(lpos Pos, left, val string) Pos {
	pos, ok := p.gotRsrv(val)
	if !ok {
		if p.recoverError() {
			return recoveredPos
		}
		p.followErr(lpos, left, val)
	}
	return pos
}

func (p *Parser) followStmts(left string, lpos Pos, stops ...string) ([]*Stmt, []Comment) {
	return p.followList(left, lpos, false, stops...)
}

// followCondStmts is like [Parser.followStmts], but for the condition of an
// `if`, `elif`, `while` or `until`. With [LangZsh] the list also ends at a
// statement which is not followed by a separator, as in
// `if [[ cond ]] { body }` or `while (( cond )) body`, so that zsh's
// alternate forms of complex commands can follow the condition, and with
// [LangKsh93] its statements cannot start with a showme `;`.
func (p *Parser) followCondStmts(left string, lpos Pos, stops ...string) ([]*Stmt, []Comment) {
	return p.followList(left, lpos, true, stops...)
}

func (p *Parser) followList(left string, lpos Pos, cond bool, stops ...string) ([]*Stmt, []Comment) {
	// Language variants disallowing empty command lists:
	// * [LangPOSIX]: "A list is a sequence of one or more AND-OR lists...".
	// * [LangBash]: "A list is a sequence of one or more pipelines..."
	// * [LangKsh93], where a lone `;` is an empty showme command instead.
	//
	// Language variants allowing empty command lists:
	// * [LangZsh]: "A list is a sequence of zero or more sublists...".
	// * [LangMirBSDKorn]: "Lists of commands can be created by separating pipelines...";
	//   note that the man page is not explicit, but the shell clearly allows e.g. `{ }`.
	if !p.showmeAllowed(cond) && p.got(semicolon) {
		if p.lang.in(LangZsh | LangMirBSDKorn) {
			return nil, nil // allow an empty list
		}
		p.followErr(lpos, left, noQuote("a statement list"))
		return nil, nil
	}
	stmts, last := p.stmtListUntil(cond, stops...)
	if len(stmts) < 1 {
		if p.lang.in(LangZsh | LangMirBSDKorn) {
			return nil, nil // allow an empty list
		}
		if p.recoverError() {
			return []*Stmt{{Position: recoveredPos}}, nil
		}
		p.followErr(lpos, left, noQuote("a statement list"))
	}
	return stmts, last
}

func (p *Parser) followWordTok(tok token, pos Pos) *Word {
	w := p.getWord()
	if w == nil {
		if p.recoverError() {
			return p.wordOne(&Lit{ValuePos: recoveredPos})
		}
		p.followErr(pos, tok, noQuote("a word"))
	}
	return w
}

func (p *Parser) stmtEnd(n Node, start, end string) Pos {
	pos, ok := p.gotRsrv(end)
	if !ok {
		if p.recoverError() {
			return recoveredPos
		}
		p.posErr(n.Pos(), "%#q statement must end with %#q", start, end)
	}
	return pos
}

func (p *Parser) quoteErr(lpos Pos, quote token) {
	p.posErr(lpos, "reached %#q without closing quote %#q", p.tok, quote)
}

func (p *Parser) matchingErr(lpos Pos, left, right token) {
	p.posErr(lpos, "reached %#q without matching %#q with %#q", p.tok, left, right)
}

func (p *Parser) matched(lpos Pos, left, right token) Pos {
	pos := p.pos
	if !p.got(right) {
		if p.recoverError() {
			return recoveredPos
		}
		p.matchingErr(lpos, left, right)
	}
	return pos
}

func (p *Parser) errPass(err error) {
	if p.err == nil {
		p.err = err
		p.bsp = uint(len(p.bs)) + 1
		p.r = runeEOF
		p.w = 1
		p.tok = _EOF
	}
}

// IsIncomplete reports whether a Parser error could have been avoided with
// extra input bytes. For example, if an [io.EOF] was encountered while there was
// an unclosed quote or parenthesis.
func IsIncomplete(err error) bool {
	perr, ok := err.(ParseError)
	return ok && perr.Incomplete
}

// TODO: probably redo with a [LangVariant] argument.
// Perhaps offer an iterator version as well.

// IsKeyword returns true if the given word is a language keyword
// in POSIX Shell or Bash.
func IsKeyword(word string) bool {
	// This list has been copied from the bash 5.1 source code, file y.tab.c +4460
	// TODO: should we include entries for zsh here? e.g. "{}", "repeat", "always", ...
	switch word {
	case
		"!",
		"[[", // only if COND_COMMAND is defined
		"]]", // only if COND_COMMAND is defined
		"case",
		"coproc", // only if COPROCESS_SUPPORT is defined
		"do",
		"done",
		"else",
		"esac",
		"fi",
		"for",
		"function",
		"if",
		"in",
		"select", // only if SELECT_COMMAND is defined
		"then",
		"time", // only if COMMAND_TIMING is defined
		"until",
		"while",
		"{",
		"}":
		return true
	}
	return false
}

// ParseError represents an error found when parsing a source file, from which
// the parser cannot recover.
type ParseError struct {
	Filename string
	Pos      Pos
	Text     string

	Incomplete bool
}

func (e ParseError) Error() string {
	if e.Filename == "" {
		return fmt.Sprintf("%s: %s", e.Pos, e.Text)
	}
	return fmt.Sprintf("%s:%s: %s", e.Filename, e.Pos, e.Text)
}

// LangError is returned when the parser encounters code that is only valid in
// other shell language variants. The error includes what feature is not present
// in the current language variant, and what languages support it.
type LangError struct {
	Filename string
	Pos      Pos

	// TODO(v4): replace the Langs slice with a single LangVariant bitset,
	// which is what the parser uses internally already.

	// Feature briefly describes which language feature caused the error.
	Feature string
	// Langs lists some of the language variants which support the feature.
	Langs []LangVariant
	// LangUsed is the language variant used which led to the error.
	LangUsed LangVariant
}

func (e LangError) Error() string {
	var sb strings.Builder
	if e.Filename != "" {
		sb.WriteString(e.Filename)
		sb.WriteString(":")
	}
	sb.WriteString(e.Pos.String())
	sb.WriteString(": ")
	sb.WriteString(e.Feature)
	if strings.HasSuffix(e.Feature, "s") {
		sb.WriteString(" are a ")
	} else {
		sb.WriteString(" is a ")
	}
	for i, lang := range e.Langs {
		if i > 0 {
			sb.WriteString("/")
		}
		sb.WriteString(lang.String())
	}
	sb.WriteString(" feature; tried parsing as ")
	sb.WriteString(e.LangUsed.String())
	return sb.String()
}

func (p *Parser) posErr(pos Pos, format string, args ...any) {
	// for i, arg := range args {
	// 	if arg, ok := arg.(fmt.Stringer); ok && arg != _EOF {
	// 		args[i] = quotedToken(arg)
	// 	}
	// }
	p.errPass(ParseError{
		Filename:   p.f.Name,
		Pos:        pos,
		Text:       fmt.Sprintf(format, args...),
		Incomplete: p.tok == _EOF && p.Incomplete(),
	})
}

func (p *Parser) curErr(format string, args ...any) {
	p.posErr(p.pos, format, args...)
}

func (p *Parser) checkLang(pos Pos, langSet LangVariant, format string, a ...any) {
	if p.lang.in(langSet) {
		return
	}
	if langBashLike.in(langSet) {
		// If we're reporting an error because a feature is for bash-like funcs,
		// just mention "bash" rather than "bash/bats" for the sake of clarity.
		langSet &^= LangBats
	}
	p.errPass(LangError{
		Filename: p.f.Name,
		Pos:      pos,
		Feature:  fmt.Sprintf(format, a...),
		Langs:    slices.Collect(langSet.bits()),
		LangUsed: p.lang,
	})
}

// stmts parses statements until EOF or one of the stop words.
// If cond is set, the statements form the condition of an `if`, `elif`,
// `while` or `until`; see [Parser.followCondStmts].
func (p *Parser) stmts(yield func(*Stmt, error) bool, cond bool, stops ...string) {
	endUnseparated := cond && p.lang.in(LangZsh)
	showme := p.showmeAllowed(cond)
	gotEnd := true
loop:
	for p.tok != _EOF {
		newLine := p.got(_Newl)
		switch p.tok {
		case _LitWord:
			for _, stop := range stops {
				if p.val == stop {
					break loop
				}
			}
			if p.val == "}" {
				p.curErr(`%#q can only be used to close a block`, rightBrace)
			}
		case rightParen:
			if p.quote == subCmd {
				break loop
			}
		case bckQuote:
			if p.backquoteEnd() {
				break loop
			}
		case dblSemicolon, semiAnd, dblSemiAnd, semiOr:
			if p.quote == switchCase {
				break loop
			}
			p.curErr("%#q can only be used in a case clause", p.tok)
		}
		if !newLine && !gotEnd {
			if endUnseparated {
				break
			}
			p.curErr("statements must be separated by &, ; or a newline")
		}
		if p.tok == _EOF {
			break
		}
		p.openNodes++
		var s *Stmt
		if showme && p.tok == semicolon {
			s = p.showmeStmt(true, false)
		} else {
			s = p.getStmt(true, false, false)
		}
		p.openNodes--
		if s == nil {
			p.invalidStmtStart()
			break
		}
		gotEnd = s.Semicolon.IsValid()
		if !yield(s, p.err) {
			break
		}
	}
}

func (p *Parser) stmtList(stops ...string) ([]*Stmt, []Comment) {
	return p.stmtListUntil(false, stops...)
}

func (p *Parser) stmtListUntil(cond bool, stops ...string) ([]*Stmt, []Comment) {
	var stmts []*Stmt
	var last []Comment
	fn := func(s *Stmt, err error) bool {
		stmts = append(stmts, s)
		return true
	}
	p.stmts(fn, cond, stops...)
	split := len(p.accComs)
	if p.tok == _LitWord && (p.val == "elif" || p.val == "else" || p.val == "fi") {
		// Split the comments, so that any aligned with an opening token
		// get attached to it. For example:
		//
		//     if foo; then
		//         # inside the body
		//     # document the else
		//     else
		//     fi
		// TODO(mvdan): look into deduplicating this with similar logic
		// in caseItems.
		for i, c := range slices.Backward(p.accComs) {
			if c.Pos().Col() != p.pos.Col() {
				break
			}
			split = i
		}
	}
	if split > 0 { // keep last nil if empty
		last = p.accComs[:split]
	}
	p.accComs = p.accComs[split:]
	return stmts, last
}

func (p *Parser) invalidStmtStart() {
	switch p.tok {
	case semicolon, and, or, andAnd, orOr, andPipe, andBang:
		p.curErr("%#q can only immediately follow a statement", p.tok)
	case rightParen:
		p.curErr("%#q can only be used to close a subshell", p.tok)
	default:
		p.curErr("%#q is not a valid start for a statement", p.tok)
	}
}

func (p *Parser) getWord() *Word {
	if w := p.wordAnyNumber(); len(w.Parts) > 0 && p.err == nil {
		return w
	}
	return nil
}

func (p *Parser) getLit() *Lit {
	if p.tok.isLit() {
		l := p.lit(p.pos, p.val)
		p.next()
		return l
	}
	return nil
}

func (p *Parser) wordParts(wps []WordPart) []WordPart {
	if p.quote == noState {
		p.quote = unquotedWordCont
		defer func() { p.quote = noState }()
	}
	wordCont := p.wordCont
	p.wordCont = true
	defer func() { p.wordCont = wordCont }()
	for {
		p.openNodes++
		p.nest()
		n := p.wordPart()
		p.unnest(1)
		p.openNodes--
		if n == nil {
			if len(wps) == 0 {
				return nil // normalize empty lists into nil
			}
			return wps
		}
		wps = append(wps, n)
		if p.spaced {
			return wps
		}
	}
}

func (p *Parser) ensureNoNested(pos Pos) {
	if p.forbidNested {
		p.posErr(pos, "expansions not allowed in heredoc words")
	}
}

func (p *Parser) wordPart() WordPart {
	switch p.tok {
	case _Lit, _LitWord, _LitRedir:
		l := p.lit(p.pos, p.val)
		p.next()
		return l
	case dollBrace:
		p.ensureNoNested(p.pos)
		switch p.r {
		case '|':
			p.checkLang(p.pos, langBashLike|LangMirBSDKorn, "`${|stmts;}`")
			fallthrough
		case ' ', '\t', '\n':
			p.checkLang(p.pos, langBashLike|langKshLike, "`${ stmts;}`")
			cs := &CmdSubst{
				Left:     p.pos,
				TempFile: p.r != '|',
				ReplyVar: p.r == '|',
			}
			old := p.preNested(subCmdBraces)
			p.rune() // don't tokenize '|'
			p.next()
			cs.Stmts, cs.Last = p.stmtList("}")
			p.postNested(old)
			pos, ok := p.gotRsrv("}")
			if !ok {
				p.matchingErr(cs.Left, dollBrace, rightBrace)
			}
			cs.Right = pos
			return cs
		default:
			if p.unexpandableStart() {
				return p.unexpandableParamExp()
			}
			return p.paramExp()
		}
	case dollDblParen, dollBrack:
		p.ensureNoNested(p.pos)
		return p.arithmExp(p.tok)
	case dollParen:
		p.ensureNoNested(p.pos)
		return p.cmdSubst()
	case dollar:
		pe := p.paramExp()
		if pe == nil { // was not actually a parameter expansion, like: "foo$"
			l := p.lit(p.pos, "$")
			p.next()
			return l
		}
		p.ensureNoNested(pe.Dollar)
		return pe
	case assgnParen:
		p.checkLang(p.pos, LangZsh, `%#q process substitutions`, p.tok)
		fallthrough
	case cmdIn, cmdOut:
		p.ensureNoNested(p.pos)
		ps := &ProcSubst{Op: ProcOperator(p.tok), OpPos: p.pos}
		old := p.preNested(subCmd)
		p.next()
		ps.Stmts, ps.Last = p.stmtList()
		p.postNested(old)
		ps.Rparen = p.matched(ps.OpPos, token(ps.Op), rightParen)
		return ps
	case sglQuote, dollSglQuote:
		sq := &SglQuoted{Left: p.pos, Dollar: p.tok == dollSglQuote}
		r := p.r
		for p.newLit(r); ; r = p.rune() {
			switch r {
			case '\\':
				if sq.Dollar {
					p.rune()
				}
			case '\'':
				sq.Right = p.nextPos()
				sq.Value = p.endLit()

				p.rune()
				p.next()
				return sq
			case escNewl:
				p.litBs = append(p.litBs, '\\', '\n')
			case runeEOF:
				p.tok = _EOF
				if p.recoverError() {
					sq.Right = recoveredPos
					return sq
				}
				p.quoteErr(sq.Pos(), sglQuote)
				return nil
			}
		}
	case dblQuote, dollDblQuote:
		if p.quote == dblQuotes {
			// p.tok == dblQuote, as "foo$" puts $ in the lit
			return nil
		}
		return p.dblQuoted()
	case bckQuote:
		if p.backquoteEnd() {
			return nil
		}
		p.ensureNoNested(p.pos)
		cs := &CmdSubst{Left: p.pos, Backquotes: true}
		old := p.preNested(subCmdBckquo)
		p.openBquotes++
		if old.quote == dblQuotes {
			p.openBquoteDbls++
		}

		// The lexer didn't call p.rune for us, so that it could have
		// the right p.openBquotes to properly handle backslashes.
		p.rune()

		p.next()
		cs.Stmts, cs.Last = p.stmtList()
		if p.tok == bckQuote && p.lastBquoteEsc < p.openBquotes-1 {
			// e.g. found ` before the nested backquote \` was closed.
			p.tok = _EOF
			p.quoteErr(cs.Pos(), bckQuote)
		}
		p.postNested(old)
		p.openBquotes--
		if old.quote == dblQuotes {
			p.openBquoteDbls--
		}
		cs.Right = p.pos

		// Like above, the lexer didn't call p.rune for us.
		p.rune()
		if !p.got(bckQuote) {
			if p.recoverError() {
				cs.Right = recoveredPos
			} else {
				p.quoteErr(cs.Pos(), bckQuote)
			}
		}
		return cs
	case leftParen:
		if (p.lang.in(LangZsh) || p.kshPatternGroup()) && p.r != ')' {
			// Zsh glob qualifier like *(N) or .(:a), or a ksh93 pattern group
			// like ~(E)re or {2}(ab); the only case where ( immediately after
			// a word is not one is () for a function declaration, which the
			// parser handles earlier.
			return p.groupLit(1)
		}
		return nil
	case dblLeftParen:
		if p.lang.in(LangZsh) {
			// A group which starts with another one, like ((a|b)|c)*.
			return p.groupLit(2)
		}
		return nil
	case globQuest, globStar, globPlus, globAt, globExcl:
		p.checkLang(p.pos, langBashLike|langKshLike, "extended globs")
		eg := &ExtGlob{Op: GlobOperator(p.tok), OpPos: p.pos}
		lparens := 1
		r := p.r
	globLoop:
		for p.newLit(r); ; r = p.rune() {
			switch r {
			case runeEOF:
				break globLoop
			case '(':
				lparens++
			case ')':
				if lparens--; lparens == 0 {
					break globLoop
				}
			}
		}
		eg.Pattern = p.lit(posAddCol(eg.OpPos, 2), p.endLit())
		p.rune()
		p.next()
		if lparens != 0 {
			p.matchingErr(eg.OpPos, token(eg.Op), rightParen)
		}
		return eg
	default:
		return nil
	}
}

// kshPatternGroup reports whether the current "(" token, right after a literal
// word part, starts one of the ksh93 pattern groups whose prefix is not one
// of the extended globs, like {2}(ab), {1,3}(ab), ~(E)re, %(pairs) or *-(x).
func (p *Parser) kshPatternGroup() bool {
	if !p.lang.in(LangKsh93) || p.tok != leftParen || p.spaced || p.val == "" {
		return false
	}
	switch p.val[len(p.val)-1] {
	case '}', '~', '%', '-':
		return true
	}
	return false
}

// groupLit lexes a zsh glob qualifier, a zsh or ksh93 parenthesized group, or
// a ksh93 pattern group as a literal, like (N), (a|(b|c)) or the (E) in
// ~(E)re, after its opening parentheses, of which there are
// depth. Nested groups and quoted strings, which glob qualifiers like
// (e:'...':) use, may contain closing parentheses.
func (p *Parser) groupLit(depth int) *Lit {
	pos := p.pos
	opening := strings.Repeat("(", depth)
	p.pos = p.nextPos()
	var quote rune
loop:
	for p.newLit(p.r); p.r != runeEOF; p.rune() {
		switch {
		case quote == '\'':
			if p.r == '\'' {
				quote = 0
			}
		case p.r == '\\':
			p.rune()
		case quote == '"':
			if p.r == '"' {
				quote = 0
			}
		case p.r == '\'', p.r == '"':
			quote = p.r
		case p.r == '(':
			depth++
		case p.r == ')':
			if depth--; depth == 0 {
				break loop
			}
		}
	}
	if p.r != ')' {
		p.tok = _EOF // we can only get here due to EOF
		p.matchingErr(pos, leftParen, rightParen)
	}
	p.rune()
	p.val = p.endLit()
	l := p.lit(pos, opening+p.val)
	p.next()
	return l
}

// arithmExp parses an arithmetic expansion starting at the current left
// token: "$((", "$[", or "((" for the bare ((expr)) of ksh93's seek
// redirections.
func (p *Parser) arithmExp(left token) *ArithmExp {
	ar := &ArithmExp{Left: p.pos, Bracket: left == dollBrack, Bare: left == dblLeftParen}
	old := p.preNested(arithmExpr)
	p.next()
	if !p.lang.in(LangZsh) && p.got(hash) {
		p.checkLang(ar.Pos(), LangMirBSDKorn, "unsigned expressions")
		ar.Unsigned = true
	}
	ar.X = p.followArithm(left, ar.Left)
	if ar.Bracket {
		if p.tok != rightBrack {
			if p.recoverError() {
				ar.Right = recoveredPos
				return ar
			}
			p.arithmMatchingErr(ar.Left, dollBrack, rightBrack)
		}
		p.postNested(old)
		ar.Right = p.pos
		p.next()
	} else {
		ar.Right = p.arithmEnd(left, ar.Left, old)
	}
	return ar
}

func (p *Parser) cmdSubst() *CmdSubst {
	cs := &CmdSubst{Left: p.pos}
	old := p.preNested(subCmd)
	p.next()
	cs.Stmts, cs.Last = p.stmtList()
	p.postNested(old)
	cs.Right = p.matched(cs.Left, dollParen, rightParen)
	return cs
}

func (p *Parser) dblQuoted() *DblQuoted {
	alloc := &struct {
		quoted DblQuoted
		parts  [1]WordPart
	}{
		quoted: DblQuoted{Left: p.pos, Dollar: p.tok == dollDblQuote},
	}
	q := &alloc.quoted
	old := p.quote
	p.quote = dblQuotes
	p.next()
	q.Parts = p.wordParts(alloc.parts[:0])
	p.quote = old
	q.Right = p.pos
	if !p.got(dblQuote) {
		if p.recoverError() {
			q.Right = recoveredPos
		} else {
			p.quoteErr(q.Pos(), dblQuote)
		}
	}
	return q
}

// unexpandableStart reports whether the ${ token just lexed starts an
// expansion that bash, POSIX shells and mksh can never expand, but which
// they only reject with "bad substitution" when the expansion runs: one that
// starts with a rune that cannot begin a parameter, like ${(%):-%x} or
// ${.sh.file}, or a special parameter followed by a word, like ${@name@}.
// Such expansions are common in scripts which run them only under zsh or
// ksh93, and in templates whose placeholders are filled in later.
func (p *Parser) unexpandableStart() bool {
	if !p.lang.in(langLazyParamExp) {
		return false
	}
	switch r := p.r; {
	case r == runeEOF || r == escNewl || paramNameRune(r) || r == '#' || r == '!':
		return false
	case r == '%':
		return !p.lang.in(LangMirBSDKorn)
	case singleRuneParam(r):
		next := p.peek()
		return paramNameRune(next) || next == '{' || next == '('
	}
	return true
}

// unexpandableParamExp parses an expansion which [Parser.unexpandableStart]
// accepted as a literal holding its text, up to the '}' that closes it
// outside of quotes and nested braces, like the shells do.
func (p *Parser) unexpandableParamExp() *Lit {
	pos := p.pos
	var text strings.Builder
	text.WriteString("${")
	depth, quote, escaped := 0, rune(0), false
	for ; ; p.rune() {
		r := p.r
		switch {
		case r == runeEOF:
			p.tok = _EOF
			p.matchingErr(pos, dollBrace, rightBrace)
			return p.lit(pos, text.String())
		case r == escNewl:
			continue
		case escaped:
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
		case r == '{':
			depth++
		case r == '}' && depth == 0:
			text.WriteRune(r)
			p.rune()
			l := p.lit(pos, text.String())
			p.next()
			return l
		case r == '}':
			depth--
		}
		text.WriteRune(r)
	}
}

// paramExp parses a short or full parameter expansion, depending on whether
// [Parser.tok] is [dollar] or [dollBrace]. It returns nil if a [dollar] token
// does not form a valid parameter expansion, in which case it should be parsed
// as a literal.
func (p *Parser) paramExp() *ParamExp {
	old := p.quote
	p.quote = runeByRune
	// [ParamExp.Short] means we are parsing $exp rather than ${exp}.
	pe := &ParamExp{
		Dollar: p.pos,
		Short:  p.tok == dollar,
	}
	if !pe.Short && p.r == '(' {
		p.checkLang(pe.Pos(), LangZsh, `parameter expansion flags`)
		// For now, for simplicity, we parse flags as just a literal.
		// In the future, parsing as a word is better for cases like
		// `${(ps.$sep.)val}`.
		lparen := p.nextPos()
		p.rune()
		p.pos = p.nextPos()
		for p.newLit(p.r); p.r != runeEOF && p.r != ')'; p.rune() {
		}
		p.val = p.endLit()
		if p.r != ')' {
			p.tok = _EOF // we can only get here due to EOF
			p.matchingErr(lparen, leftParen, rightParen)
		}
		pe.Flags = p.lit(p.pos, p.val)
		p.rune()
	}
	// Zsh-only prefixes that change how the parameter is expanded.
	// They may appear in any combination, like ${=^name}.
	// Doubling the rune (${==a}, ${~~a}, ${^^a}) forces the option off.
zshPrefixLoop:
	for p.lang.in(LangZsh) {
		var field *OptState
		switch p.r {
		case '=':
			field = &pe.Split
		case '~':
			field = &pe.GlobSubst
		case '^':
			field = &pe.RcExpand
		default:
			break zshPrefixLoop
		}
		next, after := p.peekTwo()
		state := OptOn
		check := next
		if rune(next) == p.r {
			state = OptOff
			check = after
		}
		if check == utf8.RuneSelf || check == '}' {
			break zshPrefixLoop
		}
		// For the short form, only treat as a prefix if followed by something
		// that could start a parameter name or another zsh prefix.
		if pe.Short && check != '=' && check != '~' && check != '^' &&
			!paramNameStartRune(check) {
			break zshPrefixLoop
		}
		if state == OptOff {
			p.rune() // consume the first of the doubled pair
		}
		p.rune()
		*field = state
	}
	if !pe.Short || p.lang.in(LangZsh) {
		// Prefixes, like ${#name} to get the length of a variable.
		// Note that zsh allows the short forms $#name and $+name too,
		// but not $%name nor $!name.
		switch p.r {
		case '#':
			if p.paramNameStart(pe) {
				pe.Length = true
			}
		case '%':
			if !pe.Short && p.paramNameStart(pe) {
				p.checkLang(pe.Pos(), LangMirBSDKorn, "`${%%foo}`")
				pe.Width = true
			}
		case '!':
			if !pe.Short && p.paramNameStart(pe) {
				p.checkLang(pe.Pos(), langBashLike|langKshLike, "`${!foo}`")
				pe.Excl = true
			}
		case '+':
			if p.paramNameStart(pe) {
				p.checkLang(pe.Pos(), LangZsh, "`${+foo}`")
				pe.IsSet = true
			}
		case '@':
			if p.lang.in(LangKsh93) && !pe.Short && p.paramNameStart(pe) {
				pe.Attributes = true
			}
		}
	}
	if pe = p.paramExpParameter(pe); pe == nil {
		p.quote = old
		return nil // just "$"
	}
	// In short mode, any indexing or suffixes is not allowed, and we don't require '}'.
	// Zsh is an exception: $foo[1] and $foo[1,3] are valid. Note that $1[x] does not qualify.
	if pe.Short {
		if p.lang.in(LangZsh) && p.r == '[' && (len(p.val) != 1 || !positionalRuneParam(p.val[0])) {
			p.pos = p.nextPos()
			p.rune()
			pe.Index = p.subscript(pe.Param)
			pe.Elems = p.zshSubscripts()
		}
		p.quote = old
		p.next()
		return pe
	}
	// Index expressions like ${foo[1]}. Note that expansion suffixes can be combined,
	// like ${foo[@]//replace/with}.
	if p.r == '[' {
		p.checkLang(p.nextPos(), langBashLike|langKshLike|LangZsh, "arrays")
		// In zsh some of these like ${@[-1]} or ${*[1,3]} work,
		// so we don't do this sort of check at all.
		if !p.lang.in(LangZsh) && pe.Param != nil && !p.validVarName(pe.Param.Value) {
			p.posErr(p.nextPos(), "cannot index a special parameter name")
		}
		p.pos = p.nextPos()
		p.rune()
		pe.Index = p.subscript(pe.Param)
		pe.Elems = append(p.kshRefElems(), p.zshSubscripts()...)
	}
	tokRune := p.r
	p.pos = p.nextPos()
	p.tok = p.paramToken(p.r)
	if p.tok == rightBrace {
		pe.Rbrace = p.pos
		p.quote = old
		p.next()
		return pe
	}
	// Zsh applies ${#name} to the result of any further operator,
	// so ${#name%pat} is the length of ${name%pat}.
	zshLength := pe.Length && p.lang.in(LangZsh)
	if p.tok != _EOF && (pe.Length && !zshLength || pe.Width || pe.IsSet || pe.Attributes) && !p.kshCountNames(pe) {
		p.curErr("cannot combine multiple parameter expansion operators")
	}
	switch p.tok {
	case slash, dblSlash: // pattern search and replace
		p.checkLang(p.pos, langBashLike|langKshLike|LangZsh, "search and replace")
		pe.Repl = &Replace{All: p.tok == dblSlash}
		p.paramExpReplace(pe.Repl)
	case colon: // slicing
		if p.lang.in(LangZsh) && p.r == '/' {
			p.rune()
			pe.Repl = &Replace{Whole: true}
			p.paramExpReplace(pe.Repl)
			break
		}
		if p.lang.in(LangZsh) && (p.r == '&' || asciiLetter(p.r)) {
			pos := p.pos
		loop:
			for p.newLit(p.r); ; p.rune() {
				switch p.r {
				case runeEOF:
					p.tok = _EOF
					p.matchingErr(pe.Dollar, dollBrace, rightBrace)
					break loop
				case '}':
					pe.Modifiers = append(pe.Modifiers, p.lit(pos, p.endLit()))
					pe.Rbrace = p.nextPos()
					p.rune()
					break loop
				case ':':
					pe.Modifiers = append(pe.Modifiers, p.lit(pos, p.endLit()))
					p.rune()
					pos = p.nextPos()
					p.newLit(p.r)
				}
			}
			p.quote = old
			p.next()
			return pe
		}
		p.checkLang(p.pos, langBashLike|langKshLike|LangZsh, "slicing")
		pe.Slice = &Slice{}
		colonPos := p.pos
		p.quote = paramExpSlice
		if p.next(); p.tok != colon {
			pe.Slice.Offset = p.followArithm(colon, colonPos)
		}
		colonPos = p.pos
		if p.got(colon) {
			pe.Slice.Length = p.followArithm(colon, colonPos)
		}
		// Need to use a different matched style so arithm errors
		// get reported correctly
		p.quote = old
		pe.Rbrace = p.pos
		p.matchedArithm(pe.Dollar, dollBrace, rightBrace)
		return pe
	case caret, dblCaret, comma, dblComma: // upper/lower case
		p.checkLang(p.pos, langBashLike|LangKsh93, "this expansion operator")
		pe.Exp = p.paramExpExp()
	case at, star:
		switch {
		case p.kshCountNames(pe):
			pe.Names = ParNamesOperator(p.tok)
			p.next()
		case p.tok == star && !pe.Excl:
			p.curErr("not a valid parameter expansion operator: %#q", p.tok)
		case pe.Excl && p.r == '}':
			p.checkLang(pe.Pos(), langBashLike|LangKsh93, "`${!foo%s}`", p.tok)
			pe.Names = ParNamesOperator(p.tok)
			p.next()
		case p.tok == at:
			p.checkLang(p.pos, langBashLike|LangMirBSDKorn, "this expansion operator")
			fallthrough
		default:
			pe.Exp = p.paramExpExp()
		}
	case plus, colPlus, minus, colMinus, quest, colQuest, assgn, colAssgn,
		perc, dblPerc, hash, dblHash, colHash, colPipe, colStar,
		colColAssgn, colCaret, colDblCaret:
		pe.Exp = p.paramExpExp()
	case _EOF:
	default:
		if paramNameRune(tokRune) {
			if pe.Param != nil {
				p.curErr("%#q cannot be followed by a word", pe.Param.Value)
			} else {
				p.curErr("nested parameter expansion cannot be followed by a word")
			}
		} else {
			p.curErr("not a valid parameter expansion operator: %#q", string(tokRune))
		}
	}
	if p.tok != _EOF && p.tok != rightBrace {
		p.tok = p.paramToken(p.r)
	}
	p.quote = old
	pe.Rbrace = p.matched(pe.Dollar, dollBrace, rightBrace)
	return pe
}

// kshRefElems parses the further subscripts and members which may follow the
// first subscript of a variable reference with [LangKsh93], like [j] and
// .name in a[i][j].name, while lexing rune by rune.
func (p *Parser) kshRefElems() []RefElem {
	var elems []RefElem
	for p.lang.in(LangKsh93) {
		switch p.r {
		case '[':
			p.pos = p.nextPos()
			p.rune()
			elems = append(elems, RefElem{Index: p.eitherIndex()})
		case '.':
			p.rune()
			pos := p.nextPos()
			for p.newLit(p.r); paramNameRune(p.r); p.rune() {
			}
			name := p.endLit()
			if !ValidName(name) {
				p.posErr(pos, "invalid parameter name")
				return elems
			}
			elems = append(elems, RefElem{Member: p.lit(pos, name)})
		default:
			return elems
		}
	}
	return elems
}

// zshSubscripts parses the further subscripts which may follow the first
// subscript of a parameter expansion with [LangZsh], each applying to the
// result of the one before, like [1] in ${a[2][1]} or [(I)x] in
// ${a[2,-1][(I)x]}, while lexing rune by rune.
func (p *Parser) zshSubscripts() []RefElem {
	var elems []RefElem
	for p.lang.in(LangZsh) && p.r == '[' {
		p.pos = p.nextPos()
		p.rune()
		elems = append(elems, RefElem{Index: p.eitherIndex()})
	}
	return elems
}

// kshCountNames reports whether the current token is the "@" or "*" of
// ksh93's ${#prefix@} or ${#prefix*}, which count the variables whose names
// start with prefix.
func (p *Parser) kshCountNames(pe *ParamExp) bool {
	return p.lang.in(LangKsh93) && pe.Length && (p.tok == at || p.tok == star) && p.r == '}'
}

// paramNameStart reports whether a parameter name can begin after a prefix rune
// such as the '#' in ${#foo}, consuming the prefix rune if so.
// Only the long form allows a quoted nested expansion like ${#"$(foo)"} or EOF;
// in the short form the prefix rune is the parameter itself, as in "$#" or $#"$foo".
func (p *Parser) paramNameStart(pe *ParamExp) bool {
	r := p.peek()
	if paramNameStartRune(r) || (r == '.' && !pe.Short && p.lang.in(LangKsh93)) || (!pe.Short && (r == '"' || r == utf8.RuneSelf)) {
		p.rune()
		return true
	}
	return false
}

func (p *Parser) nestedParameterStart(pe *ParamExp) (left token, quotePos Pos) {
	if pe.Short {
		return illegalTok, Pos{}
	}
	if p.r == '"' {
		quotePos = p.nextPos()
		p.rune()
	}
	if p.r != '$' {
		if quotePos.IsValid() {
			return dollar, quotePos
		}
		return illegalTok, Pos{}
	}
	switch p1 := p.peek(); p1 {
	case '{', '(':
		p.pos = p.nextPos()
		p.checkLang(p.pos, LangZsh, "nested parameter expansions")
		if p.err != nil {
			return illegalTok, Pos{} // xxx given that we overwrite p.tok below
		}
		p.rune()
		p.rune()
		if p1 == '{' {
			left = dollBrace
		} else { // '('
			left = dollParen
		}
	}
	return left, quotePos
}

func (p *Parser) paramExpParameter(pe *ParamExp) *ParamExp {
	// Check for Zsh nested parameter expressions like ${(f)"$(foo)"}.
	if left, quotePos := p.nestedParameterStart(pe); left != illegalTok {
		var wp WordPart
		p.nest()
		switch p.tok = left; p.tok {
		case dollBrace: // ${#${nested parameter}}
			p.tok = dollBrace
			wp = p.paramExp()
		case dollParen: // ${#$(nested command)}
			wp = p.cmdSubst()
		default: // dollar
			p.posErr(pe.Pos(), "invalid nested parameter expansion")
		}
		p.unnest(1)
		if quotePos.IsValid() {
			if p.r != '"' {
				p.tok = p.paramToken(p.r)
				if p.tok == illegalTok {
					p.posErr(pe.Pos(), "invalid nested parameter expansion")
				} else {
					p.quoteErr(quotePos, dblQuote)
				}
			}
			pe.NestedParam = &DblQuoted{
				Left:  quotePos,
				Right: p.nextPos(),
				Parts: []WordPart{wp},
			}
			p.rune()
		} else {
			pe.NestedParam = wp
		}
		return pe
	}
	// The parameter name itself, like $foo or $?.
	switch p.r {
	case '?', '-':
		if pe.Length && p.peek() != '}' {
			// actually ${#-default}, not ${#-}; fix the ambiguity
			pe.Length = false
			pos := p.nextPos()
			pe.Param = p.lit(posAddCol(pos, -1), "#")
			pe.Param.ValueEnd = pos
			break
		}
		fallthrough
	case '@', '*', '#', '!', '$':
		r, pos := p.r, p.nextPos()
		p.rune()
		pe.Param = p.lit(pos, string(r))
	default:
		// Note that $1a is equivalent to ${1}a, but ${1a} is not.
		// POSIX Shell says the latter is unspecified behavior, so match Bash's behavior.
		pos := p.nextPos()
		if pe.Short && p.lang.in(LangZsh) && asciiDigit(p.r) {
			// Zsh reads every digit of a short positional parameter,
			// so $10 is ${10} rather than ${1}0.
			for p.newLit(p.r); asciiDigit(p.r); p.rune() {
			}
			p.val = p.endLit()
		} else if pe.Short && singleRuneParam(p.r) {
			p.val = string(p.r)
			p.rune()
		} else {
			// With ksh93, ${a.b} and ${.sh.version} name variables too.
			dots := !pe.Short && p.lang.in(LangKsh93)
			for p.newLit(p.r); p.r != runeEOF; p.rune() {
				if !paramNameRune(p.r) && p.r != escNewl && (!dots || p.r != '.') {
					break
				}
			}
			p.val = p.endLit()
			if !numberLiteral(p.val) && !p.validVarName(p.val) && !p.kshNamePrefix(pe, p.val) {
				if pe.Short {
					return nil // just "$"
				}
				if p.lang.in(LangZsh) && p.val == "" {
					// Zsh allows omitting the parameter name, e.g. ${:-word}.
					return pe
				}
				p.posErr(pos, "invalid parameter name")
			}
		}
		pe.Param = p.lit(pos, p.val)
	}
	return pe
}

// paramExpReplace parses the pattern and replacement of a search and replace
// parameter expansion, after the slash which starts them.
func (p *Parser) paramExpReplace(repl *Replace) {
	p.quote = paramExpRepl
	p.next()
	repl.Orig = p.getWord()
	p.quote = paramExpExp
	if p.got(slash) {
		repl.With = p.getWord()
	}
}

func (p *Parser) paramExpExp() *Expansion {
	op := ParExpOperator(p.tok)
	switch op {
	case MatchEmpty, ArrayExclude, ArrayIntersect, AssignAlways, ArrayZip, ArrayZipLongest:
		p.checkLang(p.pos, LangZsh, "${name%sarg}", op)
	}
	p.quote = paramExpExp
	p.next()
	if op == OtherParamOps {
		if !p.tok.isLit() {
			p.curErr("@ expansion operator requires a literal")
		}
		switch p.val {
		case "a", "k", "u", "A", "E", "K", "L", "P", "U":
			p.checkLang(p.pos, langBashLike, "this expansion operator")
		case "#":
			p.checkLang(p.pos, LangMirBSDKorn, "this expansion operator")
		}
	}
	return &Expansion{Op: op, Word: p.getWord()}
}

// subscript parses the subscript of the array named by name, after its '['.
// The subscript of an array declared associative earlier, like with
// `declare -A name`, or of one of the associative arrays which zsh modules
// provide, like functions, is a string key, so it is parsed as a word rather
// than as an arithmetic expression; zsh subscript flags, like the `(r)` in
// ${name[(r)pattern]}, still apply to it.
func (p *Parser) subscript(name *Lit) ArithmExpr {
	if name != nil && p.assocArray(name.Value) && !(p.lang.in(LangZsh) && p.r == '(') {
		return p.keyIndex()
	}
	return p.eitherIndex()
}

// zshSpecialAssocArrays holds the associative arrays which zsh modules
// provide: zsh/parameter, zsh/zleparameter, zsh/complete, zsh/mapfile,
// zsh/terminfo, zsh/termcap, zsh/langinfo and zsh/system.
var zshSpecialAssocArrays = map[string]bool{
	"aliases": true, "builtins": true, "commands": true, "compstate": true,
	"dis_aliases": true, "dis_builtins": true, "dis_functions": true,
	"dis_functions_source": true, "dis_galiases": true, "dis_saliases": true,
	"functions": true, "functions_source": true, "galiases": true,
	"history": true, "jobdirs": true, "jobstates": true, "jobtexts": true,
	"langinfo": true, "mapfile": true, "modules": true, "nameddirs": true,
	"options": true, "parameters": true, "saliases": true, "sysparams": true,
	"termcap": true, "terminfo": true, "userdirs": true, "usergroups": true,
	"widgets": true,
}

// assocArray reports whether name is an associative array, so that its
// subscripts are string keys.
func (p *Parser) assocArray(name string) bool {
	return p.assocArrays[name] || p.lang.in(LangZsh) && zshSpecialAssocArrays[name]
}

// keyIndex parses the subscript of an associative array as a word, which
// may hold any characters, like `e/` or `a.b`, up to the ']' which closes it.
func (p *Parser) keyIndex() ArithmExpr {
	old, oldBrackets := p.quote, p.subFlagsBrackets
	lpos := p.pos
	p.quote, p.subFlagsBrackets = paramExpKey, 0
	p.next()
	var key ArithmExpr
	if w := p.getWord(); w != nil {
		key = w
	} else {
		p.followErrExp(lpos, leftBrack)
	}
	p.quote, p.subFlagsBrackets = old, oldBrackets
	p.matchedArithm(lpos, leftBrack, rightBrack)
	return key
}

func (p *Parser) eitherIndex() ArithmExpr {
	old := p.quote
	lpos := p.pos
	p.quote = paramExpArithm
	p.next()
	if p.tok == star || p.tok == at {
		p.tok, p.val = _LitWord, p.tok.String()
	}
	expr := p.followArithm(leftBrack, lpos)
	p.quote = old
	p.matchedArithm(lpos, leftBrack, rightBrack)
	return expr
}

func (p *Parser) zshSubFlags() *FlagsArithm {
	zf := &FlagsArithm{}
	// Lex flags as raw text, like paramExp does for ${(flags)...}.
	lparen := p.pos
	old := p.quote
	p.quote = runeByRune
	p.pos = p.nextPos()
	for p.newLit(p.r); p.r != runeEOF && p.r != ')'; p.rune() {
	}
	p.val = p.endLit()
	if p.r != ')' {
		p.tok = _EOF
		p.matchingErr(lparen, leftParen, rightParen)
	}
	zf.Flags = p.lit(p.pos, p.val)
	p.rune()
	// Parse the argument as a word rather than an arithmetic expression,
	// since zsh treats it as a pattern which undergoes parameter expansion,
	// command substitution and quote removal. It ends at the ',' or ']' token
	// which the arithmetic expression continues with.
	oldBrackets := p.subFlagsBrackets
	p.quote, p.subFlagsBrackets = paramExpSubFlagsArg, 0
	p.next()
	if w := p.getWord(); w != nil {
		zf.X = w
	}
	p.quote, p.subFlagsBrackets = old, oldBrackets
	return zf
}

// subFlagsBracket tracks the brackets in a [paramExpSubFlagsArg] word, such as
// the bracket expression in ${a[(r)[ab]*]}, and reports whether r still
// belongs to the word.
func (p *Parser) subFlagsBracket(r rune) bool {
	switch {
	case r == '[':
		p.subFlagsBrackets++
	case p.subFlagsBrackets == 0:
		return false
	default:
		p.subFlagsBrackets--
	}
	return true
}

// closesBlock reports whether the current token is a "}" which closes a block
// with [LangZsh], which unlike other shells does not require a separator
// before it, as in `{ local x }`.
func (p *Parser) closesBlock() bool {
	return p.lang.in(LangZsh) && p.tok == _LitWord && p.val == "}"
}

func (p *Parser) stopToken() bool {
	switch p.tok {
	case _EOF, _Newl, semicolon, and, or, andAnd, orOr, orAnd, andPipe, andBang,
		dblSemicolon, semiAnd, dblSemiAnd, semiOr, rightParen:
		return true
	case bckQuote:
		return p.backquoteEnd()
	}
	return false
}

func (p *Parser) backquoteEnd() bool {
	return p.lastBquoteEsc < p.openBquotes
}

// ValidName returns whether val is a valid name as per the POSIX spec.
func ValidName(val string) bool {
	if val == "" {
		return false
	}
	for i, r := range val {
		switch {
		case asciiLetter(r), r == '_':
		case i > 0 && asciiDigit(r):
		default:
			return false
		}
	}
	return true
}

// kshNamePrefix reports whether val is the prefix ending with a dot of
// ksh93's ${!prefix@} or ${#prefix@}, like config. in ${!config.@}, followed
// by the @ or * which ends the parameter expansion.
func (p *Parser) kshNamePrefix(pe *ParamExp, val string) bool {
	prefix, found := strings.CutSuffix(val, ".")
	return p.lang.in(LangKsh93) && (pe.Excl || pe.Length) && found &&
		p.validVarName(prefix) && (p.r == '@' || p.r == '*') && p.peek() == '}'
}

// validVarName reports whether val is a valid variable name in the language
// variant. With [LangKsh93], it may be a vname: identifiers separated by
// dots and optionally starting with one, like a.b or .sh.version.
func (p *Parser) validVarName(val string) bool {
	if !p.lang.in(LangKsh93) {
		return ValidName(val)
	}
	rest := strings.TrimPrefix(val, ".")
	for {
		ident, after, found := strings.Cut(rest, ".")
		if !ValidName(ident) {
			return false
		}
		if !found {
			return true
		}
		rest = after
	}
}

func numberLiteral[T string | []byte](val T) bool {
	if len(val) == 0 {
		return false
	}
	for _, r := range string(val) {
		if !asciiDigit(r) {
			return false
		}
	}
	return true
}

func (p *Parser) hasValidIdent() bool {
	if !p.tok.isLit() {
		return false
	}
	if end := p.eqlOffs; end > 0 {
		if p.val[end-1] == '+' && p.lang.in(langBashLike|langKshLike|LangZsh) {
			end-- // a+=x
		}
		if p.validVarName(p.val[:end]) {
			return true
		}
	} else if !p.validVarName(p.val) {
		return false // *[i]=x
	}
	return p.r == '[' // a[i]=x
}

func (p *Parser) getAssign(needEqual bool) *Assign {
	as := &Assign{}
	if p.eqlOffs > 0 { // foo=bar
		nameEnd := p.eqlOffs
		if p.lang.in(langBashLike|langKshLike|LangZsh) && p.val[p.eqlOffs-1] == '+' {
			// a+=b
			as.Append = true
			nameEnd--
		}
		as.Name = p.lit(p.pos, p.val[:nameEnd])
		// since we're not using the entire p.val
		as.Name.ValueEnd = posAddCol(as.Name.ValuePos, nameEnd)
		left := p.lit(posAddCol(p.pos, 1), p.val[p.eqlOffs+1:])
		if left.Value != "" {
			left.ValuePos = posAddCol(left.ValuePos, p.eqlOffs)
			as.Value = p.wordOne(left)
		}
		p.next()
	} else { // foo[x]=bar
		as.Name = p.lit(p.pos, p.val)
		// hasValidIdent already checks p.r is '['
		p.rune()
		p.pos = posAddCol(p.pos, 1)
		if p.lang.in(LangKsh93) {
			// Lex rune by rune to see any further subscripts or members.
			old := p.quote
			p.quote = runeByRune
			as.Index = p.subscript(as.Name)
			as.Elems = p.kshRefElems()
			p.quote = old
			p.next()
		} else {
			as.Index = p.subscript(as.Name)
		}
		if p.spaced || p.stopToken() {
			if needEqual {
				p.followErr(as.Pos(), "a[b]", assgn)
			} else {
				as.Naked = true
				return as
			}
		}
		if p.tok == assgnParen {
			if !p.lang.in(LangZsh) {
				p.curErr("arrays cannot be nested")
				return nil
			}
			// zsh allows a[i]=(values...).
			// assgnParen consumed both '=' and '(',
			// so rewrite as leftParen for array parsing below.
			p.tok = leftParen
			p.pos = posAddCol(p.pos, 1)
		} else {
			if len(p.val) > 0 && p.val[0] == '+' {
				as.Append = true
				p.val = p.val[1:]
				p.pos = posAddCol(p.pos, 1)
			}
			if len(p.val) < 1 || p.val[0] != '=' {
				if as.Append {
					p.followErr(as.Pos(), "a[b]+", assgn)
				} else {
					p.followErr(as.Pos(), "a[b]", assgn)
				}
				return nil
			}
			p.pos = posAddCol(p.pos, 1)
			p.val = p.val[1:]
			if p.val == "" {
				p.next()
			}
		}
	}
	if p.spaced || p.stopToken() {
		return as
	}
	if as.Value == nil && p.tok == leftParen {
		p.checkLang(p.pos, langBashLike|langKshLike|LangZsh, "arrays")
		if as.Array = p.arrayExpr(as.Name); as.Array == nil {
			return nil
		}
	} else if w := p.getWord(); w != nil {
		if as.Value == nil {
			as.Value = w
		} else {
			as.Value.Parts = append(as.Value.Parts, w.Parts...)
		}
	}
	return as
}

// arrayExpr parses an array expression, starting at its "(". With
// [LangKsh93], it may instead hold the list of a compound assignment, like
// `(a=1 b=2)` or `(typeset -i a=1; b=2)`, and its elements may themselves be
// compound or array values, like in `( (a=1) (a=2) )` or `([k]=(a=1))`.
// It returns nil if the elements are not valid.
func (p *Parser) arrayExpr(name *Lit) *ArrayExpr {
	ae := &ArrayExpr{Lparen: p.pos}
	newQuote := p.quote
	if p.lang.in(langBashLike | LangZsh | LangKsh93) {
		newQuote = arrayElems
	}
	old := p.preNested(newQuote)
	p.next()
	p.got(_Newl)
	if p.lang.in(LangKsh93) && p.kshCompoundStart() {
		p.quote = subCmd
		ae.Stmts, ae.Last = p.stmtList()
		p.postNested(old)
		ae.Rparen = p.matched(ae.Lparen, leftParen, rightParen)
		return ae
	}
	for p.tok != _EOF && p.tok != rightParen {
		el := &ArrayElem{}
		el.Comments, p.accComs = p.accComs, nil
		if p.tok == leftBrack {
			left := p.pos
			el.Index = p.subscript(name)
			switch {
			case p.tok != assgnParen:
				p.follow(left, `[x]`, assgn)
			case p.lang.in(LangKsh93):
				// ksh93 allows [k]=(values...), like zsh's a[i]=(values...).
				// assgnParen consumed both '=' and '(',
				// so rewrite as leftParen for the nested array below.
				p.tok = leftParen
				p.pos = posAddCol(p.pos, 1)
			default:
				p.curErr("arrays cannot be nested")
				return nil
			}
		}
		if p.lang.in(LangKsh93) && p.tok == leftParen {
			p.nest()
			el.Array = p.arrayExpr(nil)
			p.unnest(1)
			if el.Array == nil {
				return nil
			}
		} else if el.Value = p.getWord(); el.Value == nil {
			switch p.tok {
			case _Newl, rightParen, leftBrack:
				// TODO: support [index]=[
			default:
				p.curErr("array element values must be words")
				return nil
			}
		}
		if len(p.accComs) > 0 {
			c := p.accComs[0]
			if c.Pos().Line() == el.End().Line() {
				el.Comments = append(el.Comments, c)
				p.accComs = p.accComs[1:]
			}
		}
		ae.Elems = append(ae.Elems, el)
		p.got(_Newl)
	}
	ae.Last, p.accComs = p.accComs, nil
	p.postNested(old)
	ae.Rparen = p.matched(ae.Lparen, leftParen, rightParen)
	return ae
}

// kshCompoundStart reports whether the current token starts the list of a
// ksh93 compound assignment, like `a=1` in `x=(a=1 b=2)` or `typeset` in
// `x=(typeset -i a=1)`, rather than the first element of an array.
func (p *Parser) kshCompoundStart() bool {
	if !p.tok.isLit() {
		return false
	}
	if p.eqlOffs > 0 && p.hasValidIdent() {
		return true
	}
	if p.tok != _LitWord {
		return false
	}
	switch p.val {
	case "typeset", "integer", "float", "compound", "nameref", "readonly", "export", "enum", "function", ".":
		return true
	}
	return false
}

func (p *Parser) peekRedir() bool {
	switch p.tok {
	case _LitRedir, rdrOut, appOut, rdrIn, rdrInOut, dplIn, dplOut,
		rdrClob, appClob, hdoc, dashHdoc, wordHdoc,
		rdrAll, rdrAllClob, appAll, appAllClob,
		hashHdoc, rdrOutRepl, rdrInOutTr, seekIn, seekInCopy, seekOut:
		return true
	}
	return false
}

func (p *Parser) doRedirect(s *Stmt) {
	var r *Redirect
	if s.Redirs == nil {
		var alloc struct {
			redirs [4]*Redirect
			redir  Redirect
		}
		s.Redirs = alloc.redirs[:0]
		r = &alloc.redir
		s.Redirs = append(s.Redirs, r)
	} else {
		r = &Redirect{}
		s.Redirs = append(s.Redirs, r)
	}
	r.N = p.getLit()
	if r.N != nil && r.N.Value[0] == '{' {
		p.checkLang(r.N.Pos(), langBashLike|LangZsh|LangKsh93, "`{varname}` redirects")
	}
	r.Op, r.OpPos = RedirOperator(p.tok), p.pos
	switch r.Op {
	case RdrAll:
		p.checkLang(p.pos, langBashLike|langKshLike|LangZsh, "%#q redirects", r.Op)
	case AppAll:
		p.checkLang(p.pos, langBashLike|LangMirBSDKorn|LangZsh, "%#q redirects", r.Op)
	case AppClob, RdrAllClob, AppAllClob:
		p.checkLang(p.pos, LangZsh, "%#q redirects", r.Op)
	}
	p.next()
	switch r.Op {
	case Hdoc, DashHdoc, HashHdoc:
		old := p.quote
		p.quote, p.forbidNested = hdocWord, true
		p.heredocs = append(p.heredocs, r)
		r.Word = p.followWordTok(token(r.Op), r.OpPos)
		p.quote, p.forbidNested = old, false
		if p.tok == _Newl {
			if len(p.accComs) > 0 {
				c := p.accComs[0]
				if c.Pos().Line() == s.End().Line() {
					s.Comments = append(s.Comments, c)
					p.accComs = p.accComs[1:]
				}
			}
			p.doHeredocs()
		}
	case WordHdoc:
		p.checkLang(r.OpPos, langBashLike|langKshLike|LangZsh, "herestrings")
		r.Word = p.followWordTok(token(r.Op), r.OpPos)
	case SeekIn, SeekOut:
		switch {
		case p.tok == dblLeftParen:
			// ksh93's <#((expr)) seeks to the offset that expr evaluates to.
			r.Word = p.wordOne(p.arithmExp(dblLeftParen))
		case r.Op == SeekIn && p.tok == rightParen && p.quote == subCmd:
			// ksh93's $(n<#) expands to the offset of file descriptor n.
		default:
			r.Word = p.followWordTok(token(r.Op), r.OpPos)
		}
	default:
		r.Word = p.followWordTok(token(r.Op), r.OpPos)
	}
}

func (p *Parser) getStmt(readEnd, binCmd, fnBody bool) *Stmt {
	levels := 1
	p.nest()
	defer func() { p.unnest(levels) }()
	pos, ok := p.gotRsrv("!")
	s := &Stmt{Position: pos}
	if ok {
		s.Negated = true
		if p.stopToken() {
			p.posErr(s.Pos(), `%#q cannot form a statement alone`, exclMark)
		}
		if _, ok := p.gotRsrv("!"); ok {
			p.posErr(s.Pos(), `cannot negate a command multiple times`)
		}
	}
	if s = p.gotStmtPipe(s, false); s == nil || p.err != nil {
		return nil
	}
	// instead of using recursion, iterate manually
	for p.tok == andAnd || p.tok == orOr {
		if binCmd {
			// left associativity: in a list of BinaryCmds, the
			// right recursion should only read a single element
			return s
		}
		b := &BinaryCmd{
			OpPos: p.pos,
			Op:    BinCmdOperator(p.tok),
			X:     s,
		}
		levels++
		p.nest()
		p.next()
		p.got(_Newl)
		if p.showmeAllowed(false) && p.tok == semicolon {
			b.Y = p.showmeStmt(false, true)
		} else {
			b.Y = p.getStmt(false, true, false)
		}
		if b.Y == nil || p.err != nil {
			if p.recoverError() {
				b.Y = &Stmt{Position: recoveredPos}
			} else {
				p.followErr(b.OpPos, b.Op, noQuote("a statement"))
				return nil
			}
		}
		s = &Stmt{Position: s.Position}
		s.Cmd = b
		s.Comments, b.X.Comments = b.X.Comments, nil
	}
	if readEnd {
		switch p.tok {
		case semicolon:
			s.Semicolon = p.pos
			p.next()
		case and:
			s.Semicolon = p.pos
			p.next()
			s.Background = true
		case orAnd:
			s.Semicolon = p.pos
			p.next()
			s.Coprocess = true
		case andPipe, andBang:
			s.Semicolon = p.pos
			p.next()
			s.Disown = true
		}
	}
	if len(p.accComs) > 0 && !binCmd && !fnBody {
		c := p.accComs[0]
		if c.Pos().Line() == s.End().Line() {
			s.Comments = append(s.Comments, c)
			p.accComs = p.accComs[1:]
		}
	}
	return s
}

// cmdAfterRedirs reports whether cmd can follow the redirections which start
// its statement outside zsh: a simple command, and with ksh93 also a
// subshell, an arithmetic command, a declaration or a POSIX-style function
// declaration.
func (p *Parser) cmdAfterRedirs(cmd Command) bool {
	switch cmd := cmd.(type) {
	case *CallExpr:
		return true
	case *Subshell, *ArithmCmd, *DeclClause, *LetClause:
		return p.lang.in(LangKsh93)
	case *FuncDecl:
		return p.lang.in(LangKsh93) && !cmd.RsrvWord
	}
	return false
}

// showmeAllowed reports whether a statement in a list can start with ksh93's
// showme `;`, which can precede any simple command of a list outside the
// condition of an `if`, `elif`, `while` or `until`.
func (p *Parser) showmeAllowed(cond bool) bool {
	return !cond && p.lang.in(LangKsh93)
}

// showmeStmt parses a statement starting with ksh93's showme `;`: a simple
// command which does not start with a redirection, or nothing at all. The
// shell ignores the `;` unless `set -o showme` is on, which prints the
// command instead of running it.
func (p *Parser) showmeStmt(readEnd, binCmd bool) *Stmt {
	pos := p.pos
	p.next()
	if p.stopToken() || p.closingRsrv() {
		s := &Stmt{Position: pos, Showme: true}
		if readEnd && p.tok == and {
			s.Semicolon, s.Background = p.pos, true
			p.next()
		}
		return s
	}
	s := p.getStmt(readEnd, binCmd, false)
	if s == nil || p.err != nil {
		return nil
	}
	first := s
	for {
		b, ok := first.Cmd.(*BinaryCmd)
		if !ok {
			break
		}
		first = b.X
	}
	if !showmeCmd(first) {
		p.posErr(pos, "%#q can only precede a simple command", semicolon)
		return nil
	}
	s.Position, s.Showme = pos, true
	return s
}

// closingRsrv reports whether the current token is a reserved word which
// closes or continues a compound command, so that it cannot start one.
func (p *Parser) closingRsrv() bool {
	if p.tok != _LitWord {
		return false
	}
	switch p.val {
	case "}", "then", "elif", "else", "fi", "do", "done", "esac":
		return true
	}
	return false
}

// showmeCmd reports whether a statement can follow ksh93's showme `;`.
func showmeCmd(s *Stmt) bool {
	if s.Negated || (len(s.Redirs) > 0 && (s.Cmd == nil || s.Cmd.Pos().After(s.Redirs[0].Pos()))) {
		return false
	}
	switch cmd := s.Cmd.(type) {
	case *CallExpr, *DeclClause, *LetClause:
		return true
	case *FuncDecl:
		return !cmd.RsrvWord
	}
	return false
}

func (p *Parser) gotStmtPipe(s *Stmt, binCmd bool) *Stmt {
	s.Comments, p.accComs = p.accComs, nil
	for p.peekRedir() {
		p.doRedirect(s)
	}
	redirsStart := len(s.Redirs)
	switch p.tok {
	case _LitWord:
		switch p.val {
		case "{":
			p.block(s)
		case "{}":
			// Zsh treats closing braces in a special way, allowing this.
			if p.lang.in(LangZsh) {
				s.Cmd = &Block{Lbrace: p.pos, Rbrace: posAddCol(p.pos, 1)}
				p.next()
			}
		case "if":
			p.ifClause(s)
		case "while", "until":
			p.whileClause(s, p.val == "until")
		case "repeat":
			if p.lang.in(LangZsh) {
				p.repeatClause(s)
			}
		case "for":
			p.forClause(s)
		case "foreach":
			if p.lang.in(LangZsh) {
				p.foreachClause(s)
			}
		case "case":
			p.caseClause(s)
		case "}":
			p.curErr(`%#q can only be used to close a block`, rightBrace)
		case "then", "elif":
			p.curErr("%#q can only be used in an `if`", p.val)
		case "fi":
			p.curErr("%#q can only be used to end an `if`", p.val)
		case "do":
			p.curErr(`%#q can only be used in a loop`, p.val)
		case "done":
			p.curErr(`%#q can only be used to end a loop`, p.val)
		case "esac":
			p.curErr("%#q can only be used to end a `case`", p.val)
		case "!":
			if !s.Negated {
				p.curErr(`%#q can only be used in full statements`, exclMark)
				break
			}
		case "[[":
			if p.lang.in(langBashLike | langKshLike | LangZsh) {
				p.testClause(s)
			}
		case "]]":
			if p.lang.in(langBashLike | langKshLike | LangZsh) {
				p.curErr(`%#q can only be used to close a test`, dblRightBrack)
			}
		case "let":
			if p.lang.in(langBashLike | langKshLike | LangZsh) {
				p.letClause(s)
			}
		case "function":
			if p.lang.in(langBashLike | langKshLike | LangZsh) {
				p.bashFuncDecl(s)
			}
		case "declare":
			if p.lang.in(langBashLike | LangZsh) { // Note that mksh lacks this one.
				p.declClause(s)
			}
		case "local": // Note that ksh93 lacks this one.
			if p.lang.in(langBashLike | LangMirBSDKorn | LangZsh) {
				p.declClause(s)
			}
		case "export", "readonly", "typeset", "nameref":
			if p.lang.in(langBashLike | langKshLike | LangZsh) {
				p.declClause(s)
			}
		case "time":
			if p.lang.in(langBashLike | langKshLike | LangZsh) {
				p.timeClause(s)
			}
		case "namespace":
			if p.lang.in(LangKsh93) {
				p.namespaceClause(s)
			}
		case "coproc":
			switch {
			case p.lang.in(langBashLike): // Note that mksh lacks this one.
				p.coprocClause(s)
			case p.lang.in(LangZsh):
				p.zshCoprocClause(s)
			}
		case "select":
			if p.lang.in(langBashLike | langKshLike | LangZsh) {
				p.selectClause(s)
			}
		case "@test":
			if p.lang.in(LangBats) {
				p.testDecl(s)
			}
		}
		if s.Cmd != nil {
			break
		}
		if p.hasValidIdent() {
			p.callExpr(s, nil, true)
			break
		}
		name := p.lit(p.pos, p.val)
		p.next()
		// In zsh, ( after a word is a glob qualifier unless followed
		// immediately by ), which is the func declaration syntax.
		if p.tok == leftParen && (!p.lang.in(LangZsh) || p.r == ')') {
			p.next()
			p.follow(name.ValuePos, "foo(", rightParen)
			if p.lang.in(LangPOSIX) && !ValidName(name.Value) {
				p.posErr(name.Pos(), "invalid func name")
			}
			p.funcDecl(s, name.ValuePos, false, true, name)
		} else {
			w := p.wordOne(name)
			if (p.lang.in(LangZsh) || p.kshPatternGroup()) && !p.spaced {
				w.Parts = append(w.Parts, p.wordParts(nil)...)
			}
			p.callExpr(s, w, false)
		}
	case bckQuote:
		if p.backquoteEnd() {
			break
		}
		fallthrough
	case _Lit, dollBrace, dollDblParen, dollParen, dollar, cmdIn, assgnParen, cmdOut,
		sglQuote, dollSglQuote, dblQuote, dollDblQuote, dollBrack,
		globQuest, globStar, globPlus, globAt, globExcl:
		if p.hasValidIdent() {
			p.callExpr(s, nil, true)
			break
		}
		w := p.wordAnyNumber()
		if p.got(leftParen) {
			p.posErr(w.Pos(), "invalid func name")
		}
		p.callExpr(s, w, false)
	case leftParen:
		if p.r == ')' {
			p.rune()
			fpos := p.pos
			p.next()
			if p.tok == _LitWord && p.val == "{" {
				p.checkLang(fpos, LangZsh, "anonymous functions")
			}
			p.funcDecl(s, fpos, false, true)
			break
		}
		p.subshell(s)
	case dblLeftParen:
		p.arithmExpCmd(s)
	}
	if s.Cmd == nil && len(s.Redirs) == 0 {
		return nil // no statement found
	}
	if redirsStart > 0 && s.Cmd != nil && !p.cmdAfterRedirs(s.Cmd) {
		p.checkLang(s.Pos(), LangZsh, "redirects before compound commands")
	}
	for p.peekRedir() {
		p.doRedirect(s)
	}
	levels := 0
	defer func() { p.unnest(levels) }()
	// instead of using recursion, iterate manually
	for p.tok == or || p.tok == orAnd {
		if binCmd {
			// left associativity: in a list of BinaryCmds, the
			// right recursion should only read a single element
			return s
		}
		if p.tok == orAnd && p.lang.in(langKshLike) {
			// No need to check for LangPOSIX, as on that language
			// we parse |& as two tokens.
			break
		}
		b := &BinaryCmd{OpPos: p.pos, Op: BinCmdOperator(p.tok), X: s}
		levels++
		p.nest()
		p.next()
		p.got(_Newl)
		if b.Y = p.gotStmtPipe(&Stmt{Position: p.pos}, true); b.Y == nil || p.err != nil {
			if p.recoverError() {
				b.Y = &Stmt{Position: recoveredPos}
			} else {
				p.followErr(b.OpPos, b.Op, noQuote("a statement"))
				break
			}
		}
		s = &Stmt{Position: s.Position}
		s.Cmd = b
		s.Comments, b.X.Comments = b.X.Comments, nil
		// in "! x | y", the bang applies to the entire pipeline
		s.Negated = b.X.Negated
		b.X.Negated = false
	}
	return s
}

func (p *Parser) subshell(s *Stmt) {
	sub := &Subshell{Lparen: p.pos}
	old := p.preNested(subCmd)
	p.next()
	sub.Stmts, sub.Last = p.followStmts("(", sub.Lparen)
	p.postNested(old)
	sub.Rparen = p.matched(sub.Lparen, leftParen, rightParen)
	s.Cmd = sub
}

func (p *Parser) arithmExpCmd(s *Stmt) {
	ar := &ArithmCmd{Left: p.pos}
	old := p.preNested(arithmExprCmd)
	p.next()
	if !p.lang.in(LangZsh) && p.got(hash) {
		p.checkLang(ar.Pos(), LangMirBSDKorn, "unsigned expressions")
		ar.Unsigned = true
	}
	ar.X = p.followArithm(dblLeftParen, ar.Left)
	ar.Right = p.arithmEnd(dblLeftParen, ar.Left, old)
	s.Cmd = ar
}

func (p *Parser) block(s *Stmt) {
	b := p.braceGroup()
	if !p.lang.in(LangZsh) || p.tok != _LitWord || p.val != "always" {
		s.Cmd = b
		return
	}
	// Zsh runs the always list after the try list, however that one ends.
	tc := &TryClause{Try: b, AlwaysPos: p.pos}
	p.next()
	if p.tok != _LitWord || p.val != "{" {
		p.followErr(tc.AlwaysPos, "always", leftBrace)
	}
	tc.Always = p.braceGroup()
	s.Cmd = tc
}

// braceGroup parses a list of statements in braces, starting at the current
// "{" token.
func (p *Parser) braceGroup() *Block {
	b := &Block{Lbrace: p.pos}
	p.next()
	b.Stmts, b.Last = p.followStmts("{", b.Lbrace, "}")
	if pos, ok := p.gotRsrv("}"); ok {
		b.Rbrace = pos
	} else if p.recoverError() {
		b.Rbrace = recoveredPos
	} else {
		p.matchingErr(b.Lbrace, leftBrace, rightBrace)
	}
	return b
}

func (p *Parser) ifClause(s *Stmt) {
	rootIf := &IfClause{Position: p.pos}
	p.next()
	p.ifBranch(rootIf, "if")
	curIf := rootIf
	levels := 0
	defer func() { p.unnest(levels) }()
	for !curIf.Short && p.tok == _LitWord && p.val == "elif" {
		levels++
		p.nest()
		elf := &IfClause{Position: p.pos}
		curIf.Last = p.accComs
		p.accComs = nil
		p.next()
		p.ifBranch(elf, "elif")
		curIf.Else = elf
		curIf = elf
	}
	if !curIf.Short {
		if elsePos, ok := p.gotRsrv("else"); ok {
			curIf.Last = p.accComs
			p.accComs = nil
			els := &IfClause{Position: elsePos}
			// After a branch in braces, zsh allows "else { list }" without "fi";
			// otherwise, a brace group simply starts the "else" list.
			if curIf.Braces && p.tok == _LitWord && p.val == "{" {
				els.Braces = true
				els.Then, els.ThenLast, els.Rbrace = p.bracedBody(rootIf, "if")
			} else {
				els.Then, els.ThenLast = p.followStmts("else", els.Position, "fi")
			}
			curIf.Else = els
			curIf = els
		}
	}
	if curIf.Braces || curIf.Short {
		// zsh's alternate forms do not end with "fi".
		s.Cmd = rootIf
		return
	}
	curIf.Last = p.accComs
	p.accComs = nil
	rootIf.FiPos = p.stmtEnd(rootIf, "if", "fi")
	for els := rootIf.Else; els != nil; els = els.Else {
		// All the nested IfClauses share the same FiPos.
		els.FiPos = rootIf.FiPos
	}
	s.Cmd = rootIf
}

// ifBranch parses the condition and body of an "if" or "elif" branch.
func (p *Parser) ifBranch(ic *IfClause, rsrv string) {
	ic.Cond, ic.CondLast = p.followCondStmts(rsrv, ic.Position, "then")
	if p.lang.in(LangZsh) {
		switch {
		case p.tok == _LitWord && p.val == "{":
			ic.Braces = true
			ic.ThenPos = p.pos
			ic.Then, ic.ThenLast, ic.Rbrace = p.bracedBody(ic, rsrv)
			return
		case p.shortBodyStart():
			ic.Short = true
			ic.Then = p.shortBody()
			return
		}
	}
	ic.ThenPos = p.followRsrv(ic.Position, rsrv+" <cond>", "then")
	ic.Then, ic.ThenLast = p.followStmts("then", ic.ThenPos, "fi", "elif", "else")
}

// bracedBody parses the body of a complex command in braces, starting at the
// current "{" token, and returns its statements and the position of "}".
// The node and its reserved word are used for error messages.
func (p *Parser) bracedBody(n Node, rsrv string) ([]*Stmt, []Comment, Pos) {
	lbrace := p.pos
	p.next()
	stmts, last := p.followStmts("{", lbrace, "}")
	return stmts, last, p.stmtEnd(n, rsrv, "}")
}

// shortBodyStart reports whether the current token starts the single sublist
// which forms the body of one of zsh's short forms of complex commands,
// such as `if [[ cond ]] body` or `for name (words) body`.
func (p *Parser) shortBodyStart() bool {
	if p.stopToken() {
		return false
	}
	if p.tok == _LitWord {
		switch p.val {
		case "then", "elif", "else", "fi", "do", "done", "esac", "{", "}":
			return false
		}
	}
	return true
}

// shortBody parses the single sublist which forms the body of one of zsh's
// short forms of complex commands. Any separator after it is left to the
// statement holding the complex command.
func (p *Parser) shortBody() []*Stmt {
	s := p.getStmt(false, false, true)
	if s == nil {
		if p.err == nil {
			p.invalidStmtStart()
		}
		return nil
	}
	return []*Stmt{s}
}

// loopBody holds the body of a loop clause, which follows its header.
type loopBody struct {
	doPos, donePos Pos
	braces, short  bool
	stmts          []*Stmt
	last           []Comment
}

// loopBody parses the body of a loop clause: "do list done", "{ list }" with
// braceLangs, or with [LangZsh] a single sublist. A non-empty braceFeature
// names braces as a feature to report in other language variants.
// The comments before the body are moved to hoist if it is non-nil.
// The node, its reserved word, the description of its header and the
// position of the reserved word are used for error messages.
func (p *Parser) loopBody(n Node, rsrv, header string, rsrvPos Pos, braceLangs LangVariant, braceFeature string, hoist *Stmt) (b loopBody) {
	hoistComments := func() {
		if hoist != nil {
			hoist.Comments = append(hoist.Comments, p.accComs...)
			p.accComs = nil
		}
	}
	if p.tok == _LitWord && p.val == "{" {
		if braceFeature != "" {
			p.checkLang(p.pos, braceLangs, "%s", braceFeature)
		}
		if p.lang.in(braceLangs) {
			b.braces = true
			b.doPos = p.pos
			p.next()
			hoistComments()
			b.stmts, b.last = p.followStmts("{", b.doPos, "}")
			b.donePos = p.stmtEnd(n, rsrv, "}")
			return b
		}
	}
	if p.lang.in(LangZsh) && p.shortBodyStart() {
		b.short = true
		hoistComments()
		b.stmts = p.shortBody()
		return b
	}
	b.doPos = p.followRsrv(rsrvPos, header, "do")
	hoistComments()
	b.stmts, b.last = p.followStmts("do", b.doPos, "done")
	b.donePos = p.stmtEnd(n, rsrv, "done")
	return b
}

func (p *Parser) whileClause(s *Stmt, until bool) {
	wc := &WhileClause{WhilePos: p.pos, Until: until}
	rsrv := "while"
	rsrvCond := "while <cond>"
	if wc.Until {
		rsrv = "until"
		rsrvCond = "until <cond>"
	}
	p.next()
	wc.Cond, wc.CondLast = p.followCondStmts(rsrv, wc.WhilePos, "do")
	b := p.loopBody(wc, rsrv, rsrvCond, wc.WhilePos, LangZsh, "", nil)
	wc.DoPos, wc.DonePos, wc.Braces, wc.Short, wc.Do, wc.DoLast = b.doPos, b.donePos, b.braces, b.short, b.stmts, b.last
	s.Cmd = wc
}

func (p *Parser) repeatClause(s *Stmt) {
	rc := &RepeatClause{RepeatPos: p.pos}
	p.next()
	if rc.Count = p.getWord(); rc.Count == nil {
		p.followErr(rc.RepeatPos, "repeat", noQuote("a word"))
	}
	p.got(semicolon)
	p.got(_Newl)
	b := p.loopBody(rc, "repeat", "repeat word", rc.RepeatPos, LangZsh, "", nil)
	rc.DoPos, rc.DonePos, rc.Braces, rc.Short, rc.Do, rc.DoLast = b.doPos, b.donePos, b.braces, b.short, b.stmts, b.last
	s.Cmd = rc
}

func (p *Parser) forClause(s *Stmt) {
	fc := &ForClause{ForPos: p.pos}
	p.next()
	fc.Loop = p.loop(fc.ForPos)
	b := p.loopBody(fc, "for", "for foo [in words]", fc.ForPos, langBashLike|langKshLike|LangZsh, "for loops with braces", s)
	fc.DoPos, fc.DonePos, fc.Braces, fc.Short, fc.Do, fc.DoLast = b.doPos, b.donePos, b.braces, b.short, b.stmts, b.last
	s.Cmd = fc
}

func (p *Parser) foreachClause(s *Stmt) {
	fc := &ForClause{ForPos: p.pos, Foreach: true}
	p.next()
	fc.Loop = p.wordIter("foreach", fc.ForPos)
	s.Comments = append(s.Comments, p.accComs...)
	p.accComs = nil
	fc.Do, fc.DoLast = p.followStmts("foreach", fc.ForPos, "end")
	fc.DonePos = p.stmtEnd(fc, "foreach", "end")
	s.Cmd = fc
}

func (p *Parser) loop(fpos Pos) Loop {
	switch p.tok {
	case leftParen, dblLeftParen:
		p.checkLang(p.pos, langBashLike|LangZsh|LangKsh93, "c-style fors")
	}
	if p.tok == dblLeftParen {
		cl := &CStyleLoop{Lparen: p.pos}
		old := p.preNested(arithmExprCmd)
		p.next()
		cl.Init = p.arithmExpr(false)
		if !p.got(dblSemicolon) {
			p.follow(p.pos, "expr", semicolon)
			cl.Cond = p.arithmExpr(false)
			p.follow(p.pos, "expr", semicolon)
		}
		cl.Post = p.arithmExpr(false)
		cl.Rparen = p.arithmEnd(dblLeftParen, cl.Lparen, old)
		if p.got(semicolon) && p.tok == _Newl {
			// ksh93 wants `do` or `{` on the same line as the `;`.
			p.checkLang(p.pos, langBashLike|LangZsh, "a newline after `));`")
		}
		p.got(_Newl)
		return cl
	}
	return p.wordIter("for", fpos)
}

func (p *Parser) wordIter(ftok string, fpos Pos) *WordIter {
	wi := &WordIter{}
	if wi.Name = p.getLit(); wi.Name == nil {
		p.followErr(fpos, ftok, noQuote("a literal"))
	}
	if p.lang.in(LangZsh) && ftok != "select" && p.iterName() {
		// Zsh assigns the words to any number of names in turn,
		// like `for key value in ${(kv)assoc}`.
		wi.Names = []*Lit{wi.Name}
		wi.Name = nil
		for p.iterName() {
			wi.Names = append(wi.Names, p.getLit())
		}
	}
	if p.got(semicolon) {
		p.got(_Newl)
		return wi
	}
	newline := p.got(_Newl)
	if pos, ok := p.gotRsrv("in"); ok {
		wi.InPos = pos
		for !p.stopToken() {
			if w := p.getWord(); w == nil {
				p.curErr("word list can only contain words")
			} else {
				wi.Items = append(wi.Items, w)
			}
		}
		p.got(semicolon)
		p.got(_Newl)
	} else if p.lang.in(LangZsh) && p.tok == leftParen {
		wi.Lparen = p.pos
		p.next()
		for {
			p.got(_Newl)
			if p.tok == rightParen || p.tok == _EOF {
				break
			}
			w := p.getWord()
			if w == nil {
				p.curErr("word list can only contain words")
				break
			}
			wi.Items = append(wi.Items, w)
		}
		wi.Rparen = p.matched(wi.Lparen, leftParen, rightParen)
		p.got(semicolon)
		p.got(_Newl)
	} else if p.tok == _LitWord && p.val == "do" {
	} else if p.lang.in(LangZsh) && newline {
		// With zsh, a body other than do/done may follow the newline.
	} else {
		p.followErr(fpos, ftok+" foo", noQuote("`in`, `do`, `;`, or a newline"))
	}
	return wi
}

// iterName reports whether the current token is one more name for a zsh
// loop to assign words to, as opposed to what follows the names.
func (p *Parser) iterName() bool {
	if p.tok != _LitWord {
		return false
	}
	switch p.val {
	case "in", "do", "{":
		return false
	}
	return true
}

func (p *Parser) selectClause(s *Stmt) {
	fc := &ForClause{ForPos: p.pos, Select: true}
	p.next()
	fc.Loop = p.wordIter("select", fc.ForPos)
	b := p.loopBody(fc, "select", "select foo [in words]", fc.ForPos, LangZsh, "", nil)
	fc.DoPos, fc.DonePos, fc.Braces, fc.Short, fc.Do, fc.DoLast = b.doPos, b.donePos, b.braces, b.short, b.stmts, b.last
	s.Cmd = fc
}

func (p *Parser) caseClause(s *Stmt) {
	cc := &CaseClause{Case: p.pos}
	p.next()
	cc.Word = p.getWord()
	if cc.Word == nil {
		p.followErr(cc.Case, "case", noQuote("a word"))
	}
	end := "esac"
	p.got(_Newl)
	if pos, ok := p.gotRsrv("{"); ok {
		cc.In = pos
		cc.Braces = true
		p.checkLang(cc.Pos(), LangMirBSDKorn|LangZsh, "`case i {`")
		end = "}"
	} else {
		cc.In = p.followRsrv(cc.Case, "case x", "in")
	}
	cc.Items = p.caseItems(end)
	cc.Last, p.accComs = p.accComs, nil
	cc.Esac = p.stmtEnd(cc, "case", end)
	s.Cmd = cc
}

// zshCasePatterns parses the patterns of a case item with [LangZsh], up to the
// ")" which ends them, left as the current token. A leading "(" either opens
// the list of patterns, as in `(a|b) cmd`, or starts a parenthesized group
// which is part of the first pattern, as in `(a|b)*) cmd`; zsh tells them
// apart by what follows the matching ")".
func (p *Parser) zshCasePatterns() []*Word {
	var pats []*Word
	switch p.tok {
	case leftParen, dblLeftParen:
		lparen := p.pos
		group, bars, rparen := p.zshPatternGroup(lparen, p.tok == dblLeftParen)
		if p.err != nil {
			return nil
		}
		switch p.r {
		case ' ', '\t', '\r', '\n', escNewl, ';', '&', runeEOF:
			if slices.Contains(group, nil) {
				p.posErr(lparen, "case patterns must consist of words")
			}
			return group // the optional "(" before the patterns
		}
		pats = append(pats, p.zshGroupWord(lparen, group, bars, rparen))
	default:
		pats = append(pats, p.zshCasePattern())
	}
	for p.tok == or {
		p.next()
		pats = append(pats, p.zshCasePattern())
	}
	if p.tok != rightParen {
		p.curErr("case patterns must be separated with %#q", or)
	}
	return pats
}

// zshCasePattern parses a single zsh case pattern, which may start with a
// parenthesized group of patterns.
func (p *Parser) zshCasePattern() *Word {
	switch p.tok {
	case leftParen, dblLeftParen:
		lparen := p.pos
		group, bars, rparen := p.zshPatternGroup(lparen, p.tok == dblLeftParen)
		return p.zshGroupWord(lparen, group, bars, rparen)
	}
	w := p.getWord()
	if w == nil {
		p.curErr("case patterns must consist of words")
	}
	return w
}

// zshPatternGroup parses the patterns of a parenthesized group, separated by
// "|", after its "(" at lparen, and returns them with the positions of the
// "|" and of the closing ")", which is left as the current token. If nested
// is set, the "(" was lexed as part of "((", so the first pattern starts with
// another group, as in `((a|b)|c)`.
func (p *Parser) zshPatternGroup(lparen Pos, nested bool) (pats []*Word, bars []Pos, rparen Pos) {
	// A '#' directly after '(' is part of the pattern, like in `(#i)foo`.
	old := p.quote
	p.quote = unquotedWordCont
	defer func() { p.quote = old }()
	if nested {
		inner := posAddCol(lparen, 1)
		group, innerBars, innerRparen := p.zshPatternGroup(inner, false)
		pats = append(pats, p.zshGroupWord(inner, group, innerBars, innerRparen))
	} else {
		p.next()
		pats = append(pats, p.zshGroupAlternative())
	}
	for p.err == nil && p.tok == or {
		bars = append(bars, p.pos)
		p.next()
		pats = append(pats, p.zshGroupAlternative())
	}
	rparen = p.pos
	if p.err == nil && p.tok != rightParen {
		p.matchingErr(lparen, leftParen, rightParen)
	}
	return pats, bars, rparen
}

// zshGroupAlternative parses one of the patterns separated by "|" in a
// parenthesized group, which zsh allows to be empty, as in `(|l)server` or
// `(un|)install`; an empty one is returned as nil.
func (p *Parser) zshGroupAlternative() *Word {
	if p.tok == or || p.tok == rightParen {
		return nil
	}
	return p.zshCasePattern()
}

// zshGroupWord builds the word for a pattern starting with a parenthesized
// group, consuming the closing ")" and whatever continues the word after it.
func (p *Parser) zshGroupWord(lparen Pos, group []*Word, bars []Pos, rparen Pos) *Word {
	if p.err != nil {
		return nil
	}
	punct := func(pos Pos, val string) *Lit {
		return &Lit{ValuePos: pos, ValueEnd: posAddCol(pos, len(val)), Value: val}
	}
	parts := []WordPart{punct(lparen, "(")}
	for i, w := range group {
		if i > 0 {
			parts = append(parts, punct(bars[i-1], "|"))
		}
		if w != nil {
			parts = append(parts, w.Parts...)
		}
	}
	parts = append(parts, punct(rparen, ")"))
	p.next()
	if !p.spaced {
		parts = p.wordParts(parts)
	}
	return &Word{Parts: parts}
}

func (p *Parser) caseItems(stop string) (items []*CaseItem) {
	p.got(_Newl)
	for p.tok != _EOF && (p.tok != _LitWord || p.val != stop) {
		ci := &CaseItem{}
		ci.Comments, p.accComs = p.accComs, nil
		if p.lang.in(LangZsh) {
			ci.Patterns = p.zshCasePatterns()
		} else {
			p.got(leftParen)
			for p.tok != _EOF {
				if w := p.getWord(); w == nil {
					p.curErr("case patterns must consist of words")
				} else {
					ci.Patterns = append(ci.Patterns, w)
				}
				if p.tok == rightParen {
					break
				}
				if !p.got(or) {
					p.curErr("case patterns must be separated with %#q", or)
				}
			}
		}
		old := p.preNested(switchCase)
		p.next()
		ci.Stmts, ci.Last = p.stmtList(stop)
		p.postNested(old)
		switch p.tok {
		case dblSemicolon, semiAnd, dblSemiAnd, semiOr:
		default:
			ci.Op = Break
			items = append(items, ci)
			return items
		}
		ci.Last = append(ci.Last, p.accComs...)
		p.accComs = nil
		ci.OpPos = p.pos
		ci.Op = CaseOperator(p.tok)
		if ci.Op == Resume {
			p.checkLang(ci.OpPos, langBashLike, "%#q", ci.Op)
		}
		p.next()
		p.got(_Newl)

		// Split the comments:
		//
		// case x in
		// a)
		//   foo
		//   ;;
		//   # comment for a
		// # comment for b
		// b)
		//   [...]
		split := len(p.accComs)
		for i, c := range slices.Backward(p.accComs) {
			if c.Pos().Col() != p.pos.Col() {
				break
			}
			split = i
		}
		ci.Comments = append(ci.Comments, p.accComs[:split]...)
		p.accComs = p.accComs[split:]

		items = append(items, ci)
	}
	return items
}

func (p *Parser) testClause(s *Stmt) {
	tc := &TestClause{Left: p.pos}
	old := p.preNested(testExpr)
	p.next()
	if tc.X = p.testExprBinary(false); tc.X == nil {
		p.followErrExp(tc.Left, dblLeftBrack)
	}
	tc.Right = p.pos
	if _, ok := p.gotRsrv("]]"); !ok {
		p.matchingErr(tc.Left, dblLeftBrack, dblRightBrack)
	}
	p.postNested(old)
	s.Cmd = tc
}

func (p *Parser) testExprBinary(pastAndOr bool) TestExpr {
	p.got(_Newl)
	var left TestExpr
	if pastAndOr {
		left = p.testExprUnary()
	} else {
		left = p.testExprBinary(true)
	}
	if left == nil {
		return left
	}
	p.got(_Newl)
	switch p.tok {
	case andAnd, orOr:
	case _LitWord:
		if p.val == "]]" {
			return left
		}
		if lw, ok := left.(*Word); ok && testBinaryOp(p.val) == 0 && p.zshModuleTestName(p.val) {
			return p.zshInfixModuleTest(lw)
		}
		if p.tok = token(testBinaryOp(p.val)); p.tok == illegalTok {
			p.curErr("not a valid test operator: %#q", p.val)
		}
	case rdrIn, rdrOut:
	case _EOF, rightParen:
		return left
	case _Lit:
		p.curErr("test operator words must consist of a single literal")
	default:
		p.curErr("not a valid test operator: %#q", p.tok)
	}
	b := &BinaryTest{
		OpPos: p.pos,
		Op:    BinTestOperator(p.tok),
		X:     left,
	}
	switch b.Op {
	case AndTest, OrTest:
		p.next()
		p.nest()
		b.Y = p.testExprBinary(false)
		p.unnest(1)
		if b.Y == nil {
			p.followErrExp(b.OpPos, b.Op)
		}
	case TsReMatch:
		p.checkLang(p.pos, langBashLike|LangZsh|LangKsh93, "regex tests")
		p.rxOpenParens = 0
		p.rxFirstPart = true
		// TODO(mvdan): Using nested states within a regex will break in
		// all sorts of ways. The better fix is likely to use a stop
		// token, like we do with heredocs.
		p.quote = testExprRegexp
		fallthrough
	default:
		if _, ok := b.X.(*Word); !ok {
			p.posErr(b.OpPos, "expected %#q, %#q or %#q after complex expr",
				AndTest, OrTest, dblRightBrack)
		}
		p.next()
		b.Y = p.followWordTok(token(b.Op), b.OpPos)
	}
	return b
}

func (p *Parser) testExprUnary() TestExpr {
	switch p.tok {
	case _EOF, rightParen:
		return nil
	case _LitWord:
		op := token(testUnaryOp(p.val))
		switch op {
		case illegalTok:
		case tsRefVar: // not available in mksh nor zsh
			if p.lang.in(langBashLike | LangKsh93) {
				p.tok = op
			}
		case tsModif: // not available in mksh
			if p.lang.in(langBashLike | LangZsh | LangKsh93) {
				p.tok = op
			}
		default:
			p.tok = op
		}
	}
	switch p.tok {
	case exclMark:
		u := &UnaryTest{OpPos: p.pos, Op: TsNot}
		p.next()
		p.nest()
		u.X = p.testExprBinary(false)
		p.unnest(1)
		if u.X == nil {
			p.followErrExp(u.OpPos, u.Op)
		}
		return u
	case tsExists, tsRegFile, tsDirect, tsCharSp, tsBlckSp, tsNmPipe,
		tsSocket, tsSmbLink, tsSticky, tsGIDSet, tsUIDSet, tsGrpOwn,
		tsUsrOwn, tsModif, tsRead, tsWrite, tsExec, tsNoEmpty,
		tsFdTerm, tsEmpStr, tsNempStr, tsOptSet, tsVarSet, tsRefVar:
		u := &UnaryTest{OpPos: p.pos, Op: UnTestOperator(p.tok)}
		p.next()
		u.X = p.followWordTok(token(u.Op), u.OpPos)
		return u
	case leftParen:
		pe := &ParenTest{Lparen: p.pos}
		p.next()
		p.nest()
		pe.X = p.testExprBinary(false)
		p.unnest(1)
		if pe.X == nil {
			p.followErrExp(pe.Lparen, leftParen)
		}
		pe.Rparen = p.matched(pe.Lparen, leftParen, rightParen)
		return pe
	case _LitWord:
		if p.val == "]]" {
			return nil
		}
		fallthrough
	default:
		if w := p.getWord(); w != nil {
			if mt := p.zshPrefixModuleTest(w); mt != nil {
				return mt
			}
			return w
		}
		// otherwise we'd return a typed nil above
		return nil
	}
}

// zshModuleTestName reports whether name, read where a test operator could
// be, names a zsh condition provided by a module: zsh accepts any word
// starting with '-' there and looks the condition up when the test runs.
func (p *Parser) zshModuleTestName(name string) bool {
	return p.lang.in(LangZsh) && len(name) > 1 && name[0] == '-'
}

// zshPrefixModuleTest parses the arguments of a prefix zsh condition provided
// by a module, like [[ -prefix -c ]] or [[ -between 1 a b ]], after its name
// w. It returns nil if w is not such a name, or if what follows is a binary
// operator, in which case w is its left operand.
func (p *Parser) zshPrefixModuleTest(w *Word) *ModuleTest {
	name, ok := w.Parts[0].(*Lit)
	if len(w.Parts) != 1 || !ok || !p.zshModuleTestName(name.Value) || testUnaryOp(name.Value) != 0 {
		return nil
	}
	if p.tok == _LitWord && testBinaryOp(p.val) != 0 {
		return nil
	}
	mt := &ModuleTest{Name: name}
	for !p.testOperandsEnd() {
		arg := p.getWord()
		if arg == nil {
			break
		}
		mt.Args = append(mt.Args, arg)
	}
	if len(mt.Args) == 0 {
		return nil
	}
	return mt
}

// zshInfixModuleTest parses an infix zsh condition provided by a module, like
// [[ $s -pcre-match pat ]], whose name is the current token, after its left
// operand.
func (p *Parser) zshInfixModuleTest(left *Word) *ModuleTest {
	mt := &ModuleTest{Left: left, Name: p.lit(p.pos, p.val)}
	p.next()
	if arg := p.getWord(); arg != nil {
		mt.Args = []*Word{arg}
	} else {
		p.followErr(mt.Name.Pos(), mt.Name.Value, noQuote("a word"))
	}
	return mt
}

// testOperandsEnd reports whether the current token ends the operands of a
// test expression.
func (p *Parser) testOperandsEnd() bool {
	switch p.tok {
	case _EOF, _Newl, andAnd, orOr, rightParen:
		return true
	case _LitWord:
		return p.val == "]]"
	}
	return false
}

func (p *Parser) declClause(s *Stmt) {
	ds := &DeclClause{Variant: p.lit(p.pos, p.val)}
	p.next()
	p.declArgs(ds)
	p.recordKshTypes(ds)
	s.Cmd = ds
}

// recordKshTypes remembers the types which a declaration clause defines with
// [LangKsh93], like Pt_t in `typeset -T Pt_t=(x=0)` or Color_t in
// `enum Color_t=(red green)`, so that later commands named after them are
// parsed as declarations.
func (p *Parser) recordKshTypes(ds *DeclClause) {
	if !p.lang.in(LangKsh93) {
		return
	}
	switch ds.Variant.Value {
	case "enum":
	case "typeset":
		if !slices.ContainsFunc(ds.Args, func(as *Assign) bool {
			if as == nil || !as.Naked || as.Value == nil {
				return false
			}
			flags := as.Value.Lit()
			return strings.HasPrefix(flags, "-") && strings.ContainsRune(flags, 'T')
		}) {
			return
		}
	default:
		return
	}
	for _, as := range ds.Args {
		if as == nil || as.Name == nil || as.Array == nil {
			continue
		}
		if p.kshTypes == nil {
			p.kshTypes = make(map[string]bool)
		}
		p.kshTypes[as.Name.Value] = true
	}
}

// declArgs parses the arguments of a declaration clause. The names it
// declares after an option like -A are recorded as associative arrays
// before their values are parsed, as in `declare -A m=([k/]=v)`.
func (p *Parser) declArgs(ds *DeclClause) {
	assoc := false
	for !p.stopToken() && !p.peekRedir() && !p.closesBlock() {
		if p.hasValidIdent() {
			if assoc {
				p.recordAssocArray(p.assignedName())
			}
			ds.Args = append(ds.Args, p.getAssign(false))
		} else if p.tok == _LitWord && p.validVarName(p.val) {
			if assoc {
				p.recordAssocArray(p.val)
			}
			ds.Args = append(ds.Args, &Assign{
				Naked: true,
				Name:  p.getLit(),
			})
		} else if w := p.getWord(); w != nil {
			if flags := w.Lit(); len(flags) > 1 && flags[0] == '-' && strings.ContainsRune(flags[1:], 'A') {
				assoc = true
			}
			ds.Args = append(ds.Args, &Assign{
				Naked: true,
				Value: w,
			})
		} else {
			p.followErr(p.pos, ds.Variant.Value, noQuote("names or assignments"))
		}
	}
}

// assignedName returns the name assigned by the current token, which
// [Parser.hasValidIdent] has accepted, like the `a` in `a=1`, `a+=1` or `a[i]=1`.
func (p *Parser) assignedName() string {
	if p.eqlOffs > 0 {
		return strings.TrimSuffix(p.val[:p.eqlOffs], "+")
	}
	return p.val
}

func (p *Parser) recordAssocArray(name string) {
	if p.assocArrays == nil {
		p.assocArrays = make(map[string]bool)
	}
	p.assocArrays[name] = true
}

// kshTypeDecl turns a call with [LangKsh93] into a declaration clause when
// it is named after a type defined earlier with enum or typeset -T, or when
// one of its arguments, the current token, is an assignment of a compound or
// array value. Such a command is a declaration command created by enum or
// typeset -T, as in `Pt_t p=(x=1 y=2)`, which ksh93 parses as one once its
// type is defined, even if the type is defined in another file. It reports
// whether it did so.
func (p *Parser) kshTypeDecl(s *Stmt, ce *CallExpr) bool {
	if !p.lang.in(LangKsh93) || len(ce.Assigns) > 0 || len(ce.Args) == 0 {
		return false
	}
	names, ok := funcNames(ce.Args[:1])
	if !ok {
		return false
	}
	if !p.kshTypes[names[0].Value] && (p.r != '(' || p.eqlOffs != len(p.val)-1 || !p.hasValidIdent()) {
		return false
	}
	ds := &DeclClause{Variant: names[0]}
	for _, w := range ce.Args[1:] {
		if lits, ok := funcNames([]*Word{w}); ok && p.validVarName(lits[0].Value) {
			ds.Args = append(ds.Args, &Assign{Naked: true, Name: lits[0]})
		} else {
			ds.Args = append(ds.Args, &Assign{Naked: true, Value: w})
		}
	}
	p.declArgs(ds)
	p.recordKshTypes(ds)
	s.Cmd = ds
	return true
}

func isBashCompoundCommand(tok token, val string) bool {
	switch tok {
	case leftParen, dblLeftParen:
		return true
	case _LitWord:
		switch val {
		case "{", "if", "while", "until", "for", "case", "[[",
			"coproc", "let", "function", "declare", "local",
			"export", "readonly", "typeset", "nameref":
			return true
		}
	}
	return false
}

func (p *Parser) timeClause(s *Stmt) {
	tc := &TimeClause{Time: p.pos}
	p.next()
	if _, ok := p.gotRsrv("-p"); ok {
		tc.PosixFormat = true
	}
	if !p.closesBlock() {
		tc.Stmt = p.gotStmtPipe(&Stmt{Position: p.pos}, false)
	}
	s.Cmd = tc
}

func (p *Parser) coprocClause(s *Stmt) {
	cc := &CoprocClause{Coproc: p.pos}
	if p.next(); isBashCompoundCommand(p.tok, p.val) {
		// has no name
		cc.Stmt = p.gotStmtPipe(&Stmt{Position: p.pos}, false)
		s.Cmd = cc
		return
	}
	cc.Name = p.getWord()
	cc.Stmt = p.gotStmtPipe(&Stmt{Position: p.pos}, false)
	if cc.Stmt == nil {
		if cc.Name == nil {
			p.posErr(cc.Coproc, "coproc clause requires a command")
			return
		}
		// name was in fact the stmt
		cc.Stmt = &Stmt{Position: cc.Name.Pos()}
		cc.Stmt.Cmd = p.call(cc.Name)
		cc.Name = nil
	} else if cc.Name != nil {
		if call, ok := cc.Stmt.Cmd.(*CallExpr); ok {
			// name was in fact the start of a call
			call.Args = append([]*Word{cc.Name}, call.Args...)
			cc.Name = nil
		}
	}
	s.Cmd = cc
}

// zshCoprocClause parses a zsh coproc clause, which unlike bash's takes no
// name: the rest of the pipeline is the coprocess.
func (p *Parser) namespaceClause(s *Stmt) {
	nc := &NamespaceClause{Namespace: p.pos}
	p.next()
	if nc.Name = p.getLit(); nc.Name == nil {
		p.followErr(nc.Namespace, "namespace", noQuote("a name"))
	}
	if p.tok != _LitWord || p.val != "{" {
		p.followErr(nc.Namespace, "namespace foo", leftBrace)
	}
	nc.Body = p.braceGroup()
	s.Cmd = nc
}

func (p *Parser) zshCoprocClause(s *Stmt) {
	cc := &CoprocClause{Coproc: p.pos}
	p.next()
	if cc.Stmt = p.gotStmtPipe(&Stmt{Position: p.pos}, false); cc.Stmt == nil {
		p.posErr(cc.Coproc, "coproc clause requires a command")
	}
	s.Cmd = cc
}

func (p *Parser) letClause(s *Stmt) {
	lc := &LetClause{Let: p.pos}
	old := p.preNested(arithmExprLet)
	p.next()
	for !p.stopToken() && !p.peekRedir() && !p.closesBlock() {
		x := p.arithmExpr(true)
		if x == nil {
			break
		}
		lc.Exprs = append(lc.Exprs, x)
	}
	if len(lc.Exprs) == 0 {
		p.followErrExp(lc.Let, "let")
	}
	p.postNested(old)
	s.Cmd = lc
}

func (p *Parser) bashFuncDecl(s *Stmt) {
	fpos := p.pos
	p.next()
	names := make([]*Lit, 0, 1)
	var params []*Lit
	for p.tok == _LitWord && p.val != "{" {
		names = append(names, p.lit(p.pos, p.val))
		p.next()
	}
	hasParens := p.got(leftParen)
	switch len(names) {
	case 0:
		if hasParens || (p.tok == _LitWord && p.val == "{") {
			p.checkLang(fpos, LangZsh, "anonymous functions")
		} else if !p.lang.in(LangZsh) {
			p.followErr(fpos, "function", noQuote("a name"))
		}
		names = nil // avoid non-nil zero-length slices
	case 1:
		// allowed in all variants
	default:
		if p.lang.in(LangKsh93) && strings.HasPrefix(names[0].Value, ".sh.math.") {
			// The other words name the arguments of an arithmetic function.
			params = names[1:]
			names = names[:1]
			break
		}
		p.checkLang(fpos, LangZsh, "multi-name functions")
	}
	if hasParens {
		p.follow(fpos, "function foo(", rightParen)
	}
	p.funcDecl(s, fpos, true, hasParens, names...).Params = params
}

func (p *Parser) testDecl(s *Stmt) {
	td := &TestDecl{Position: p.pos}
	p.next()
	if td.Description = p.getWord(); td.Description == nil {
		p.followErr(td.Position, "@test", noQuote("a description word"))
	}
	if td.Body = p.getStmt(false, false, true); td.Body == nil {
		p.followErr(td.Position, `@test "desc"`, noQuote("a statement"))
	}
	s.Cmd = td
}

func (p *Parser) unexpectedInCallExpr(ce *CallExpr) {
	// Note that we'll only keep the first error that happens.
	if len(ce.Args) > 0 {
		if cmd := ce.Args[0].Lit(); isBashCompoundCommand(_LitWord, cmd) {
			p.checkLang(p.pos, langBashLike, "the %#q builtin", cmd)
		}
	}
	p.curErr("a command can only contain words and redirects; encountered %#q", p.tok)
}

func (p *Parser) callExpr(s *Stmt, w *Word, assign bool) {
	ce := p.call(w)
	redirsStart := len(s.Redirs)
	if w == nil {
		ce.Args = ce.Args[:0]
	}
	if assign {
		ce.Assigns = append(ce.Assigns, p.getAssign(true))
	}
loop:
	for {
		switch p.tok {
		case _EOF, _Newl, semicolon, and, or, andAnd, orOr, orAnd, andPipe, andBang,
			dblSemicolon, semiAnd, dblSemiAnd, semiOr:
			break loop
		case _LitWord:
			if p.kshTypeDecl(s, ce) {
				return
			}
			if len(ce.Args) == 0 && p.hasValidIdent() {
				ce.Assigns = append(ce.Assigns, p.getAssign(true))
				break
			}
			// Avoid failing later with the confusing "} can only be used to close a block".
			if p.val == "{" && w != nil && w.Lit() == "function" {
				p.checkLang(p.pos, langBashLike, `the "function" builtin`)
			}
			// Zsh does not require a semicolon to close a block.
			if p.lang.in(LangZsh) && p.val == "}" {
				break loop
			}
			w := p.wordOne(p.lit(p.pos, p.val))
			p.next()
			if (p.lang.in(LangZsh) || p.kshPatternGroup()) && !p.spaced {
				w.Parts = append(w.Parts, p.wordParts(nil)...)
			}
			ce.Args = append(ce.Args, w)
		case _Lit:
			if len(ce.Args) == 0 && p.hasValidIdent() {
				ce.Assigns = append(ce.Assigns, p.getAssign(true))
				break
			}
			ce.Args = append(ce.Args, p.wordAnyNumber())
		case bckQuote:
			if p.backquoteEnd() {
				break loop
			}
			fallthrough
		case dollBrace, dollDblParen, dollParen, dollar, cmdIn, assgnParen, cmdOut,
			sglQuote, dollSglQuote, dblQuote, dollDblQuote, dollBrack,
			globQuest, globStar, globPlus, globAt, globExcl:
			ce.Args = append(ce.Args, p.wordAnyNumber())
		case dblLeftParen:
			if p.lang.in(LangZsh) {
				ce.Args = append(ce.Args, p.wordAnyNumber())
				break
			}
			p.curErr("%#q can only be used to open an arithmetic cmd", p.tok)
		case leftParen:
			if p.lang.in(LangZsh) && p.r != ')' {
				ce.Args = append(ce.Args, p.wordAnyNumber())
				break
			}
			if p.lang.in(LangZsh) && len(ce.Assigns) == 0 && len(s.Redirs) == redirsStart {
				// Zsh defines many functions at once with `foo bar () { ... }`.
				if names, ok := funcNames(ce.Args); ok {
					p.next()
					p.follow(names[0].ValuePos, "foo(", rightParen)
					p.funcDecl(s, names[0].ValuePos, false, true, names...)
					return
				}
			}
			p.unexpectedInCallExpr(ce)
		case rightParen:
			if p.quote == subCmd {
				break loop
			}
			p.unexpectedInCallExpr(ce)
		default:
			if p.peekRedir() {
				p.doRedirect(s)
				continue
			}
			p.unexpectedInCallExpr(ce)
		}
	}
	if len(ce.Args) == 0 {
		ce.Args = nil
	} else if !p.lang.in(LangZsh) {
		// Zsh allows arrays and elements in assignments before a command,
		// like `a=(1 2) cmd`, which apply for the command's duration.
		for _, asgn := range ce.Assigns {
			if asgn.Index != nil || asgn.Array != nil {
				p.posErr(asgn.Pos(), "inline variables cannot be arrays")
			}
		}
	}
	s.Cmd = ce
}

func (p *Parser) funcDecl(s *Stmt, pos Pos, long, withParens bool, names ...*Lit) *FuncDecl {
	fd := &FuncDecl{
		Position: pos,
		RsrvWord: long,
		Parens:   withParens,
	}
	if len(names) == 1 {
		fd.Name = names[0]
	} else {
		fd.Names = names
	}
	if p.lang.in(LangZsh) {
		// Zsh allows a separator before the body, as in `function f; cmd`.
		p.got(semicolon)
	}
	p.got(_Newl)
	// TODO: reject any body which isn't a compound command, like a quoted word
	if fd.Body = p.getStmt(false, false, true); fd.Body == nil {
		p.followErr(fd.Pos(), "foo()", noQuote("a statement"))
	}
	if len(names) == 0 && p.lang.in(LangZsh) {
		// Zsh runs an anonymous function right away,
		// passing it any words which follow its body.
		p.funcArgs(s, fd)
	}
	s.Cmd = fd
	return fd
}

// funcArgs parses the arguments which follow the body of an anonymous
// function with [LangZsh], adding any redirects among them to s.
func (p *Parser) funcArgs(s *Stmt, fd *FuncDecl) {
	for {
		if p.peekRedir() {
			p.doRedirect(s)
			continue
		}
		if p.closesBlock() {
			return
		}
		w := p.getWord()
		if w == nil {
			return
		}
		fd.Args = append(fd.Args, w)
	}
}

// funcNames returns the names of the words before `()` in a zsh function
// declaration such as `foo bar () { ... }`, if they are all literal.
func funcNames(words []*Word) ([]*Lit, bool) {
	names := make([]*Lit, 0, len(words))
	for _, w := range words {
		if len(w.Parts) != 1 {
			return nil, false
		}
		lit, ok := w.Parts[0].(*Lit)
		if !ok {
			return nil, false
		}
		names = append(names, lit)
	}
	return names, true
}
