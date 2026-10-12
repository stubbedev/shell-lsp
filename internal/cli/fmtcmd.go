package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/project"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const fmtUsage = `usage: shell-lsp fmt [flags] [path ...]

Formats shell scripts like shfmt, with the same flags and exit codes. With
no paths, or a single '-', it formats standard input. Directories are
searched for shell scripts.

  --version  show version and exit

  -l[=0], --list[=0]  error with a list of files whose formatting differs;
                      paths are separated by a newline or a null character if -l=0
  -w,     --write     write result to file instead of stdout
  -d,     --diff      error with a diff when the formatting differs
  --apply-ignore      always apply EditorConfig ignore rules
  --filename str      provide a name for the standard input file

Parser options:

  -ln, --language-dialect str  bash/posix/mksh/bats/zsh/ksh/dash, default "auto"
  -p,  --posix                 shorthand for -ln=posix
  -s,  --simplify              simplify the code

Printer options:

  -i,  --indent uint       0 for tabs (default), >0 for number of spaces
  -bn, --binary-next-line  binary ops like && and | may start a line
  -ci, --case-indent       switch cases will be indented
  -sr, --space-redirects   redirect operators will be followed by a space
  -kp, --keep-padding      keep column alignment paddings
  -fn, --func-next-line    function opening braces are placed on a separate line
  -mn, --minify            minify the code to reduce its size (implies -s)

Utilities:

  -f[=0], --find[=0]  recursively find all shell files and print the paths;
                      paths are separated by a newline or a null character if -f=0

Formatting options are read from EditorConfig files unless a parser or
printer flag is given. Exit status: 0 success, 1 errors or differences
reported by -l or -d, 2 bad command line.
`

const stdinDisplayName = "<standard input>"

type listMode uint8

const (
	listOff listMode = iota
	listLines
	listNull
)

type listFlag struct {
	mode *listMode
}

func (f listFlag) IsBoolFlag() bool { return true }

func (f listFlag) String() string {
	if f.mode == nil || *f.mode == listOff {
		return "false"
	}
	return "true"
}

func (f listFlag) Set(value string) error {
	switch value {
	case "0":
		*f.mode = listNull
		return nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("want true, false or 0, not %q", value)
	}
	*f.mode = listOff
	if b {
		*f.mode = listLines
	}
	return nil
}

func (m listMode) separator() string {
	if m == listNull {
		return "\x00"
	}
	return "\n"
}

type language struct {
	dialect dialect.Dialect
	variant syntax.LangVariant
}

func languageNamed(name string) (*language, error) {
	switch name {
	case "auto":
		return nil, nil
	case "mksh":
		return &language{dialect: dialect.Ksh, variant: syntax.LangMirBSDKorn}, nil
	case "bats":
		return &language{dialect: dialect.Bash, variant: syntax.LangBats}, nil
	}
	d, ok := dialect.FromName(name)
	if !ok {
		return nil, fmt.Errorf("unknown shell language variant: %q", name)
	}
	return &language{dialect: d, variant: d.Language()}, nil
}

type fmtOptions struct {
	list        listMode
	find        listMode
	write       bool
	diff        bool
	applyIgnore bool
	filename    string
	language    *language
	style       format.Options
	styleFlags  bool
	version     bool
	paths       []string
}

var styleFlagNames = []string{
	"ln", "language-dialect", "p", "posix", "s", "simplify", "i", "indent", "bn", "binary-next-line",
	"ci", "case-indent", "sr", "space-redirects", "kp", "keep-padding", "fn", "func-next-line", "mn", "minify",
}

func parseFmtArgs(args []string, stderr io.Writer) (fmtOptions, error) {
	var o fmtOptions
	fs := flag.NewFlagSet("shell-lsp fmt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, fmtUsage) }
	alias := func(short, long string, register func(name string)) {
		register(short)
		register(long)
	}
	boolean := func(dst *bool, short, long string) {
		alias(short, long, func(name string) { fs.BoolVar(dst, name, false, "") })
	}
	alias("l", "list", func(name string) { fs.Var(listFlag{&o.list}, name, "") })
	alias("f", "find", func(name string) { fs.Var(listFlag{&o.find}, name, "") })
	boolean(&o.write, "w", "write")
	boolean(&o.diff, "d", "diff")
	fs.BoolVar(&o.applyIgnore, "apply-ignore", false, "")
	fs.StringVar(&o.filename, "filename", "", "")
	fs.BoolVar(&o.version, "version", false, "")
	alias("ln", "language-dialect", func(name string) {
		fs.Func(name, "", func(v string) error {
			lang, err := languageNamed(v)
			o.language = lang
			return err
		})
	})
	posix := false
	boolean(&posix, "p", "posix")
	boolean(&o.style.Simplify, "s", "simplify")
	alias("i", "indent", func(name string) {
		fs.Func(name, "", func(v string) error {
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				return fmt.Errorf("want a number of spaces, not %q", v)
			}
			o.style.Indent = uint(n)
			return nil
		})
	})
	boolean(&o.style.BinaryNextLine, "bn", "binary-next-line")
	boolean(&o.style.SwitchCaseIndent, "ci", "case-indent")
	boolean(&o.style.SpaceRedirects, "sr", "space-redirects")
	boolean(&o.style.KeepPadding, "kp", "keep-padding")
	boolean(&o.style.FunctionNextLine, "fn", "func-next-line")
	boolean(&o.style.Minify, "mn", "minify")
	if err := fs.Parse(args); err != nil {
		return fmtOptions{}, err
	}
	fs.Visit(func(f *flag.Flag) {
		for _, name := range styleFlagNames {
			if f.Name == name {
				o.styleFlags = true
			}
		}
	})
	if posix {
		o.language, _ = languageNamed("posix")
	}
	if o.style.Minify {
		o.style.Simplify = true
	}
	o.paths = fs.Args()
	return o, nil
}

func runFmt(env Env, args []string) int {
	opts, err := parseFmtArgs(args, env.Stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return statusOK
	case err != nil:
		return statusUsage
	case opts.version:
		fmt.Fprintln(env.Stdout, versionPrefix+env.Version)
		return statusOK
	}
	f := &formatter{env: env, opts: opts, editor: editorconfig.NewResolver(regularfile.ReadConfig, time.Now, time.Hour)}
	if len(opts.paths) == 0 || (len(opts.paths) == 1 && opts.paths[0] == stdinName) {
		return f.stdin()
	}
	return f.files()
}

type formatter struct {
	env    Env
	opts   fmtOptions
	editor *editorconfig.Resolver
	status int
}

type formatted struct {
	name   string
	path   string
	before string
	after  string
	skip   bool
	err    error
}

func (f *formatter) fail(format string, args ...any) {
	fmt.Fprintf(f.env.Stderr, format+"\n", args...)
	f.status = statusFailed
}

func (f *formatter) stdin() int {
	if f.opts.write {
		f.fail("-w cannot be used on standard input")
		return f.status
	}
	raw, err := io.ReadAll(f.env.Stdin)
	if err != nil {
		f.fail("reading standard input: %v", err)
		return f.status
	}
	name, path := stdinDisplayName, ""
	if f.opts.filename != "" {
		name, path = f.opts.filename, f.abs(f.opts.filename)
	}
	f.report(f.format(name, path, string(raw), f.opts.applyIgnore))
	return f.status
}

type fmtTarget struct {
	name        string
	path        string
	applyIgnore bool
}

func (f *formatter) files() int {
	var targets []fmtTarget
	for _, operand := range f.opts.paths {
		path := f.abs(operand)
		info, err := os.Stat(path)
		switch {
		case err != nil:
			f.fail("%s: %v", operand, err)
		case info.IsDir():
			targets = append(targets, f.walk(operand, path)...)
		default:
			targets = append(targets, fmtTarget{name: operand, path: path, applyIgnore: f.opts.applyIgnore})
		}
	}
	if f.opts.find != listOff {
		for _, t := range targets {
			f.print(t.name + f.opts.find.separator())
		}
		return f.status
	}
	results := make([]formatted, len(targets))
	parallel.For(runtime.GOMAXPROCS(0), len(targets), func(i int) {
		t := targets[i]
		raw, _, err := workspace.ReadDisk(t.path)
		if err != nil {
			results[i] = formatted{name: t.name, err: err}
			return
		}
		results[i] = f.format(t.name, t.path, string(raw), t.applyIgnore)
	})
	for _, r := range results {
		f.report(r)
	}
	return f.status
}

func (f *formatter) walk(operand, dir string) []fmtTarget {
	walker := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.DetectDialect)
	walker.SetRoots([]string{dir})
	paths, err := walker.AllShellFiles(context.Background())
	if err != nil {
		f.fail("%s: %v", operand, err)
		return nil
	}
	out := make([]fmtTarget, 0, len(paths))
	for _, path := range paths {
		if _, embedded := host.FromName(path); embedded {
			continue
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			f.fail("%s: %v", path, err)
			continue
		}
		out = append(out, fmtTarget{name: filepath.Join(operand, rel), path: path, applyIgnore: true})
	}
	return out
}

func (f *formatter) abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(f.env.Dir, path)
}

func (f *formatter) format(name, path, raw string, applyIgnore bool) formatted {
	out := formatted{name: name, path: path, before: raw}
	if language, embedded := host.FromName(path); embedded && path != "" {
		out.err = fmt.Errorf("a %s embeds shell in its own syntax; shell-lsp fmt formats only shell scripts", language)
		return out
	}
	doc := docstore.NewDocument("", "", 0, raw)
	in := dialect.Input{Text: doc.Text()}
	if path != "" {
		in.URI = fileuri.FromPath(path)
	}
	var forced *dialect.Dialect
	if f.opts.language != nil {
		forced = &f.opts.language.dialect
	}
	resolved, err := project.Resolve(f.editor, path, forced, in)
	if err != nil {
		out.err = err
		return out
	}
	if applyIgnore && resolved.Settings.Ignore {
		out.skip = true
		return out
	}
	d, style := resolved.Dialect, resolved.Settings.Style.Options()
	if f.opts.styleFlags {
		d, style = dialect.Detect(forced, in), f.opts.style
	}
	variant := parser.Variant(d, in)
	if f.opts.language != nil {
		variant = f.opts.language.variant
	}
	tree := parser.ParseEdit(nil, doc.Text(), d, variant)
	if len(tree.Errors) > 0 {
		e := tree.Errors[0]
		out.err = &scriptError{at: placeOf(doc, e.Span.Start), message: e.Message}
		return out
	}
	out.after, out.err = format.Format(tree, style)
	return out
}

type scriptError struct {
	at      place
	message string
}

func (e *scriptError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.at.line, e.at.column, e.message)
}

func (f *formatter) print(s string) {
	if _, err := io.WriteString(f.env.Stdout, s); err != nil {
		f.fail("writing standard output: %v", err)
	}
}

func (f *formatter) report(r formatted) {
	if scriptErr, ok := errors.AsType[*scriptError](r.err); ok {
		f.fail("%s:%v", r.name, scriptErr)
		return
	}
	switch {
	case r.err != nil:
		f.fail("%s: %v", r.name, r.err)
		return
	case r.skip:
		return
	}
	changed := r.after != r.before
	if f.opts.list != listOff && changed {
		f.print(r.name + f.opts.list.separator())
		if !f.opts.write {
			f.status = statusFailed
		}
	}
	if f.opts.write && changed {
		if err := writeAtomic(r.path, []byte(r.after)); err != nil {
			f.fail("%s: %v", r.name, err)
		}
	}
	if f.opts.diff && changed {
		f.print("diff " + r.name + ".orig " + r.name + "\n" + format.Unified(r.name+".orig", r.name, r.before, r.after))
		f.status = statusFailed
	}
	if f.opts.list == listOff && !f.opts.write && !f.opts.diff {
		f.print(r.after)
	}
}

func writeAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temporary file next to %s: %w", path, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("copying the mode of %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	committed = true
	return nil
}
