package cli

import (
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const (
	checkStatusFindings = 1
	checkStatusFiles    = 2
	checkStatusSyntax   = 3
	checkStatusOptions  = 4
)

const checkUsage = `usage: shell-lsp check [OPTIONS...] FILE|DIR|- ...

Lints shell scripts like shellcheck, with the same options, output formats
and exit codes. Directories are searched for shell scripts.

  -a                  --check-sourced            Include warnings from sourced files
  -C[WHEN]            --color[=WHEN]             Use color (auto, always, never)
  -i CODE1,CODE2..    --include=CODE1,CODE2..    Consider only given types of warnings
  -e CODE1,CODE2..    --exclude=CODE1,CODE2..    Exclude types of warnings
                      --extended-analysis=bool   Perform dataflow analysis (default true)
  -f FORMAT           --format=FORMAT            Output format (checkstyle, diff, gcc, json, json1, quiet, sarif, tty)
                      --list-optional            List checks disabled by default
                      --norc                     Don't look for .shellcheckrc files
                      --rcfile=RCFILE            Prefer the specified configuration file over searching for one
  -o check1,check2..  --enable=check1,check2..   List of optional checks to enable (or 'all')
  -P SOURCEPATHS      --source-path=SOURCEPATHS  Specify path when looking for sourced files ("SCRIPTDIR" for script's dir)
  -s SHELLNAME        --shell=SHELLNAME          Specify dialect (sh, bash, dash, ksh, busybox, zsh)
  -S SEVERITY         --severity=SEVERITY        Minimum severity of errors to consider (error, warning, info, style)
  -V                  --version                  Print version information
  -W NUM              --wiki-link-count=NUM      The number of wiki links to show, when applicable
  -x                  --external-sources         Allow 'source' outside of FILES (always on)
                      --help                     Show this usage summary and exit

Exit status: 0 no findings, 1 findings, 2 unreadable files, 3 bad command
line, 4 bad option values or configuration.
`

type colorMode uint8

const (
	colorAuto colorMode = iota
	colorAlways
	colorNever
)

var colorModes = map[string]colorMode{"auto": colorAuto, "always": colorAlways, "never": colorNever}

type rcMode uint8

const (
	rcSearch rcMode = iota
	rcNone
	rcExplicit
)

type rcChoice struct {
	mode rcMode
	path string
}

type codeRange struct {
	from, to lint.Code
}

type codeSet struct {
	all    bool
	ranges []codeRange
}

func (s codeSet) has(code lint.Code) bool {
	return s.all || slices.ContainsFunc(s.ranges, func(r codeRange) bool { return r.from <= code && code <= r.to })
}

func (s codeSet) union(other codeSet) codeSet {
	return codeSet{all: s.all || other.all, ranges: slices.Concat(s.ranges, other.ranges)}
}

const allCodes = "all"

func parseCodeSet(list string) (codeSet, error) {
	var set codeSet
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		if item == allCodes {
			set.all = true
			continue
		}
		from, to, isRange := strings.Cut(item, "-")
		first, err := lint.ParseCode(from)
		if err != nil {
			return codeSet{}, err
		}
		last := first
		if isRange {
			if last, err = lint.ParseCode(to); err != nil {
				return codeSet{}, err
			}
		}
		set.ranges = append(set.ranges, codeRange{from: first, to: last})
	}
	return set, nil
}

type checkOptions struct {
	format       outputFormat
	shell        *dialect.Dialect
	severity     lint.Severity
	exclude      codeSet
	include      *codeSet
	enable       []string
	extended     *bool
	sourcePaths  []string
	checkSourced bool
	color        colorMode
	wikiLinks    int
	rc           rcChoice
	listOptional bool
	version      bool
	help         bool
	operands     []string
}

const defaultWikiLinks = 3

func severityNamed(name string) (lint.Severity, bool) {
	for severity, l := range levels {
		if l.name == name && name != "" {
			return lint.Severity(severity), true
		}
	}
	return 0, false
}

var checkOptionTable = []option[checkOptions]{
	{'a', "check-sourced", noArg, func(o *checkOptions, _ string) error { o.checkSourced = true; return nil }},
	{'C', "color", optionalArg, func(o *checkOptions, v string) error {
		if v == "" {
			o.color = colorAlways
			return nil
		}
		mode, ok := colorModes[v]
		if !ok {
			return badValue("unknown value for --color: %q. Valid options are: auto, always, never", v)
		}
		o.color = mode
		return nil
	}},
	{'i', "include", requiredArg, func(o *checkOptions, v string) error {
		set, err := parseCodeSet(v)
		if err != nil {
			return badSyntax("--include: %w", err)
		}
		if o.include != nil {
			set = o.include.union(set)
		}
		o.include = &set
		return nil
	}},
	{'e', "exclude", requiredArg, func(o *checkOptions, v string) error {
		set, err := parseCodeSet(v)
		if err != nil {
			return badSyntax("--exclude: %w", err)
		}
		o.exclude = o.exclude.union(set)
		return nil
	}},
	{0, "extended-analysis", requiredArg, func(o *checkOptions, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return badValue("--extended-analysis must be true or false, not %q", v)
		}
		o.extended = &b
		return nil
	}},
	{'f', "format", requiredArg, func(o *checkOptions, v string) error {
		i := slices.IndexFunc(outputFormats, func(f outputFormat) bool { return f.name == v })
		if i < 0 {
			names := make([]string, len(outputFormats))
			for j, f := range outputFormats {
				names[j] = f.name
			}
			slices.Sort(names)
			return badValue("unknown format %q; supported formats are %s", v, strings.Join(names, ", "))
		}
		o.format = outputFormats[i]
		return nil
	}},
	{0, "list-optional", noArg, func(o *checkOptions, _ string) error { o.listOptional = true; return nil }},
	{0, "norc", noArg, func(o *checkOptions, _ string) error { o.rc = rcChoice{mode: rcNone}; return nil }},
	{0, "rcfile", requiredArg, func(o *checkOptions, v string) error {
		if o.rc.mode != rcNone {
			o.rc = rcChoice{mode: rcExplicit, path: v}
		}
		return nil
	}},
	{'o', "enable", requiredArg, func(o *checkOptions, v string) error {
		o.enable = append(o.enable, splitList(v)...)
		return nil
	}},
	{'P', "source-path", requiredArg, func(o *checkOptions, v string) error {
		o.sourcePaths = append(o.sourcePaths, filepath.SplitList(v)...)
		return nil
	}},
	{'s', "shell", requiredArg, func(o *checkOptions, v string) error {
		d, ok := dialect.FromName(v)
		if !ok {
			return badValue("unknown shell %q", v)
		}
		o.shell = &d
		return nil
	}},
	{'S', "severity", requiredArg, func(o *checkOptions, v string) error {
		severity, ok := severityNamed(v)
		if !ok {
			return badValue("unknown value for --severity: %q. Valid options are: error, warning, info, style", v)
		}
		o.severity = severity
		return nil
	}},
	{'V', "version", noArg, func(o *checkOptions, _ string) error { o.version = true; return nil }},
	{'W', "wiki-link-count", requiredArg, func(o *checkOptions, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return badSyntax("--wiki-link-count needs a count, not %q", v)
		}
		o.wikiLinks = n
		return nil
	}},
	{'x', "external-sources", noArg, func(*checkOptions, string) error { return nil }},
	{0, "help", noArg, func(o *checkOptions, _ string) error { o.help = true; return nil }},
}

func splitList(v string) []string {
	var out []string
	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseCheckArgs(args []string) (checkOptions, error) {
	opts := checkOptions{format: outputFormats[0], severity: lint.SeverityStyle, wikiLinks: defaultWikiLinks}
	operands, err := parseGNU(args, checkOptionTable, &opts)
	if err != nil {
		return checkOptions{}, err
	}
	opts.operands = operands
	return opts, nil
}

func checkStatusOf(err error) int {
	if arg, ok := errors.AsType[*argError](err); ok {
		return arg.status
	}
	return checkStatusSyntax
}
