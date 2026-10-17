package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/atomicfile"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/project"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

var fmtUsage = fmt.Sprintf(`usage: shell-lsp fmt [flags] [path ...]

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

  -ln, --language-dialect str  %s, default "auto"
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
`, strings.Join(fmtLanguages(), "/"))

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

func languageNamed(name string) (*dialect.Shell, error) {
	var shell dialect.Shell
	switch name {
	case autoLanguage:
		return nil, nil
	}
	if i := slices.IndexFunc(fmtFlavors, func(f fmtFlavor) bool { return f.name == name }); i >= 0 {
		shell = fmtFlavors[i].flavor.Shell()
		return &shell, nil
	}
	d, ok := dialect.FromName(name)
	if !ok {
		return nil, fmt.Errorf("unknown shell language variant: %q; want %s", name, strings.Join(fmtLanguages(), ", "))
	}
	shell = dialect.Shell{Dialect: d}
	return &shell, nil
}

const autoLanguage = "auto"

type fmtFlavor struct {
	name   string
	flavor dialect.Flavor
}

var fmtFlavors = []fmtFlavor{{"mksh", dialect.FlavorMksh}, {"bats", dialect.FlavorBats}}

func fmtLanguages() []string {
	names := dialect.Names()
	for _, f := range fmtFlavors {
		names = append(names, f.name)
	}
	return append(names, autoLanguage)
}

type fmtOptions struct {
	list        listMode
	find        listMode
	write       bool
	diff        bool
	applyIgnore bool
	filename    string
	language    *dialect.Shell
	style       format.Options
	styleFlags  bool
	version     bool
	paths       []string
}

func parseFmtArgs(args []string, stderr io.Writer) (fmtOptions, error) {
	var o fmtOptions
	posix := false
	fs, styleFlags := fmtFlags(&o, &posix, stderr)
	if err := fs.Parse(args); err != nil {
		return fmtOptions{}, err
	}
	fs.Visit(func(f *flag.Flag) { o.styleFlags = o.styleFlags || styleFlags[f.Name] })
	if posix {
		o.language = &dialect.Shell{Dialect: dialect.Posix}
	}
	if o.style.Minify {
		o.style.Simplify = true
	}
	o.paths = fs.Args()
	return o, nil
}

func fmtFlags(o *fmtOptions, posix *bool, stderr io.Writer) (*flag.FlagSet, map[string]bool) {
	fs := flag.NewFlagSet("shell-lsp fmt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, fmtUsage) }
	styleFlags := map[string]bool{}
	alias := func(short, long string, register func(name string)) {
		register(short)
		register(long)
	}
	boolean := func(dst *bool, short, long string) {
		alias(short, long, func(name string) { fs.BoolVar(dst, name, false, "") })
	}
	style := func(short, long string, register func(name string)) {
		styleFlags[short], styleFlags[long] = true, true
		alias(short, long, register)
	}
	styleBoolean := func(dst *bool, short, long string) {
		style(short, long, func(name string) { fs.BoolVar(dst, name, false, "") })
	}
	alias("l", "list", func(name string) { fs.Var(listFlag{&o.list}, name, "") })
	alias("f", "find", func(name string) { fs.Var(listFlag{&o.find}, name, "") })
	boolean(&o.write, "w", "write")
	boolean(&o.diff, "d", "diff")
	fs.BoolVar(&o.applyIgnore, "apply-ignore", false, "")
	fs.StringVar(&o.filename, "filename", "", "")
	fs.BoolVar(&o.version, "version", false, "")
	style("ln", "language-dialect", func(name string) {
		fs.Func(name, "", func(v string) error {
			lang, err := languageNamed(v)
			o.language = lang
			return err
		})
	})
	styleBoolean(posix, "p", "posix")
	styleBoolean(&o.style.Simplify, "s", "simplify")
	style("i", "indent", func(name string) {
		fs.Func(name, "", func(v string) error {
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				return fmt.Errorf("want a number of spaces, not %q", v)
			}
			o.style.Indent = uint(n)
			return nil
		})
	})
	styleBoolean(&o.style.BinaryNextLine, "bn", "binary-next-line")
	styleBoolean(&o.style.SwitchCaseIndent, "ci", "case-indent")
	styleBoolean(&o.style.SpaceRedirects, "sr", "space-redirects")
	styleBoolean(&o.style.KeepPadding, "kp", "keep-padding")
	styleBoolean(&o.style.FunctionNextLine, "fn", "func-next-line")
	styleBoolean(&o.style.Minify, "mn", "minify")
	return fs, styleFlags
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
		name, path = f.opts.filename, absolute(f.env.Dir, f.opts.filename)
	}
	f.report(f.format(target{name: name, path: path}, string(raw)))
	return f.status
}

func (f *formatter) files() int {
	targets, failures := collectTargets(f.env.Dir, f.opts.paths, func(path string) bool {
		language, embedded := host.FromName(path)
		return !embedded || language.Formats()
	})
	for _, failure := range failures {
		f.fail("%s", failure)
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
		results[i] = f.format(t, string(raw))
	})
	for _, r := range results {
		f.report(r)
	}
	return f.status
}

func (f *formatter) format(t target, raw string) formatted {
	out := formatted{name: t.name, path: t.path, before: raw}
	if language, embedded := host.FromName(t.path); embedded && t.path != "" {
		return f.formatHost(out, t, language, raw)
	}
	var forced *dialect.Detection
	if f.opts.language != nil {
		forced = dialect.Forced(f.opts.language.Dialect, dialect.SourceOverride)
	}
	file, resolved, err := parseText(t.path, raw, func(in dialect.Input) (project.Resolution, error) {
		resolved, err := project.Resolve(f.editor, t.path, forced, in)
		switch {
		case f.opts.language != nil:
			resolved.Shell = *f.opts.language
		case f.opts.styleFlags:
			resolved.Detection = dialect.Resolve(nil, in)
		}
		return resolved, err
	})
	if err != nil {
		out.err = err
		return out
	}
	if (t.walked || f.opts.applyIgnore) && resolved.Settings.Ignore {
		out.skip = true
		return out
	}
	if len(file.Tree.Errors) > 0 {
		e := file.Tree.Errors[0]
		out.err = &scriptError{at: placeOf(file.Doc, e.Span.Start), message: e.Message}
		return out
	}
	style := resolved.Settings.Style.Options()
	if f.opts.styleFlags {
		style = f.opts.style
	}
	out.after, out.err = format.Format(file.Tree, style)
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
		if err := atomicfile.Write(r.path, []byte(r.after)); err != nil {
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

func (f *formatter) formatHost(out formatted, t target, language host.Language, raw string) formatted {
	if !language.Formats() {
		out.err = fmt.Errorf("a %s embeds shell in its own syntax; shell-lsp fmt formats only shell scripts and justfiles", language)
		return out
	}
	resolved, err := project.Resolve(f.editor, t.path, nil, dialect.Input{Text: raw})
	if err != nil {
		out.err = err
		return out
	}
	if (t.walked || f.opts.applyIgnore) && resolved.Settings.Ignore {
		out.skip = true
		return out
	}
	out.after, out.err = language.Format(raw)
	return out
}
