package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
)

const (
	checkStatusFindings = 1
	checkStatusFiles    = 2
	checkStatusSyntax   = 3
	checkStatusOptions  = 4
)

var checkUsage = fmt.Sprintf(`usage: shell-lsp check [OPTIONS...] FILE|DIR|- ...

Lints shell scripts like shellcheck, with the same options, output formats
and exit codes. Directories are searched for shell scripts.

  -a                  --check-sourced            Include warnings from sourced files
  -C[WHEN]            --color[=WHEN]             Use color (%s)
  -i CODE1,CODE2..    --include=CODE1,CODE2..    Consider only given types of warnings
  -e CODE1,CODE2..    --exclude=CODE1,CODE2..    Exclude types of warnings
                      --extended-analysis=bool   Perform dataflow analysis (default true)
  -f FORMAT           --format=FORMAT            Output format (%s)
                      --list-optional            List checks disabled by default
                      --norc                     Don't look for .shellcheckrc files
                      --rcfile=RCFILE            Prefer the specified configuration file over searching for one
  -o check1,check2..  --enable=check1,check2..   List of optional checks to enable (or 'all')
                      --profile=PROFILE          Lint profile (%s; default %s)
  -P SOURCEPATHS      --source-path=SOURCEPATHS  Specify path when looking for sourced files ("SCRIPTDIR" for script's dir)
  -s SHELLNAME        --shell=SHELLNAME          Specify dialect (%s)
  -S SEVERITY         --severity=SEVERITY        Minimum severity of errors to consider (%s)
  -V                  --version                  Print version information
  -W NUM              --wiki-link-count=NUM      The number of wiki links to show, when applicable
  -x                  --external-sources         Allow 'source' outside of FILES (always on)
                      --help                     Show this usage summary and exit

Exit status: 0 no findings, 1 findings, 2 unreadable files, 3 bad command
line, 4 bad option values or configuration.
`, listed(colorNames[:]), listed(formatNames()), listed(lint.ProfileNames()), lint.ProfileRecommended, listed(dialect.Names()), listed(lint.SeverityNames()))

func listed(names []string) string { return strings.Join(names, ", ") }

type colorMode uint8

const (
	colorAuto colorMode = iota
	colorAlways
	colorNever
)

var colorNames = [...]string{colorAuto: "auto", colorAlways: "always", colorNever: "never"}

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

type checkOptions struct {
	format       outputFormat
	shell        *dialect.Dialect
	severity     lint.Severity
	exclude      lint.CodeSet
	include      *lint.CodeSet
	enable       []string
	profile      lint.Profile
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

var checkOptionTable = []option[checkOptions]{
	{'a', "check-sourced", noArg, func(o *checkOptions, _ string) error { o.checkSourced = true; return nil }},
	{'C', "color", optionalArg, func(o *checkOptions, v string) error {
		if v == "" {
			o.color = colorAlways
			return nil
		}
		i := slices.Index(colorNames[:], v)
		if i < 0 {
			return badValue("unknown value for --color: %q. Valid options are: %s", v, listed(colorNames[:]))
		}
		o.color = colorMode(i)
		return nil
	}},
	{'i', "include", requiredArg, func(o *checkOptions, v string) error {
		set, err := lint.ParseCodeSet(v)
		if err != nil {
			return badSyntax("--include: %w", err)
		}
		if o.include != nil {
			set = o.include.Union(set)
		}
		o.include = &set
		return nil
	}},
	{'e', "exclude", requiredArg, func(o *checkOptions, v string) error {
		set, err := lint.ParseCodeSet(v)
		if err != nil {
			return badSyntax("--exclude: %w", err)
		}
		o.exclude = o.exclude.Union(set)
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
			return badValue("unknown format %q; supported formats are %s", v, listed(formatNames()))
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
		o.enable = append(o.enable, shellcheckrc.SplitList(v)...)
		return nil
	}},
	{0, "profile", requiredArg, func(o *checkOptions, v string) error {
		profile, ok := lint.ProfileNamed(v)
		if !ok {
			return badValue("unknown profile %q; supported profiles are %s", v, listed(lint.ProfileNames()))
		}
		o.profile = profile
		return nil
	}},
	{'P', "source-path", requiredArg, func(o *checkOptions, v string) error {
		o.sourcePaths = append(o.sourcePaths, filepath.SplitList(v)...)
		return nil
	}},
	{'s', "shell", requiredArg, func(o *checkOptions, v string) error {
		d, ok := dialect.FromName(v)
		if !ok {
			return badValue("unknown shell %q; supported shells are %s", v, listed(dialect.Names()))
		}
		o.shell = &d
		return nil
	}},
	{'S', "severity", requiredArg, func(o *checkOptions, v string) error {
		severity, ok := lint.SeverityNamed(v)
		if !ok {
			return badValue("unknown value for --severity: %q. Valid options are: %s", v, listed(lint.SeverityNames()))
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
