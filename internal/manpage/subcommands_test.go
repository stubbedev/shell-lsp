package manpage

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const toolPage = `.TH TOOL 1
.SH "NAME"
tool \- manage widgets
.SH "SYNOPSIS"
.HP \w'\fBtool\fR\ 'u
\fBtool\fR [OPTIONS...] COMMAND [UNIT...]
.SH "DESCRIPTION"
.PP
Run this:
.RS 4
.nf
tool start web
.fi
.RE
.SH "COMMANDS"
.PP
The following commands are understood:
.SS "Unit Commands"
.PP
\fBstart\fR \fIUNIT\fR\&...
.RS 4
Start one or more units\&. Units are started in parallel\&.
.sp
Waits for completion\&.
.RE
.PP
\fBstop\fR [\fIUNIT\fR\&...]
.RS 4
Stop units\&.
.PP
\fB\-\-force\fR
.RS 4
Kill instead of asking politely\&.
.RE
.PP
\fBtrue\fR
.RS 4
A value, not a command\&.
.RE
.RE
.PP
\fBsync\fR
.RS 4
Synchronise state\&.
.RE
.PP
\fBtool\-sync\-all\fR(1)
.RS 4
Synchronise everything\&.
.RE
.PP
\fBtoolconfig\fR(5)
.RS 4
A configuration file format\&.
.RE
.PP
\fBq\fR
.RS 4
A single letter is never a subcommand\&.
.RE
.SH "OPTIONS"
.PP
\fB\-v\fR, \fB\-\-verbose\fR
.RS 4
Print more\&.
.RE
`

const toolStartPage = `.TH TOOL-START 1
.SH NAME
tool-start \- start widget units
.SH SYNOPSIS
.B tool start
[\fB\-w\fR] \fIUNIT\fR...
.SH OPTIONS
.TP
.B \-w, \-\-wait
wait until the units are up
`

const toolExtraPage = `.TH TOOL-EXTRA 1
.SH NAME
tool-extra \- extra widget tricks
.SH SYNOPSIS
.B tool extra
[\fB\-x\fR]
.SH OPTIONS
.TP
.B \-x
do the extra trick
`

const toolHelperPage = `.TH TOOL-HELPER 1
.SH NAME
tool-helper \- a separate program that shares the prefix
.SH SYNOPSIS
.B tool-helper
[\fB\-h\fR]
`

const archivePage = `.Dd March 1, 2026
.Dt ARCHIVE 8
.Os
.Sh NAME
.Nm archive
.Nd store files
.Sh SYNOPSIS
.Nm
.Cm add
.Ar file ...
.Nm
.Cm list
.Sh SUBCOMMANDS
.Bl -tag -width Ds
.It Nm Cm add Oo Fl f Oc Ar file ...
Add files to the archive.
.Bl -tag -width Ds
.It Fl f
Overwrite existing members.
.El
.It Nm Cm list
List the members.
.El
`

const pagerPage = `.TH PAGER 1
.SH NAME
pager \- page through text
.SH SYNOPSIS
.B pager
[\fB\-s\fR] [\fIfile\fR...]
.SH COMMANDS
.TP
.B search
find text interactively
.TP
.B quit
leave the pager
`

func subcommandNames(subs []Subcommand) []string {
	names := make([]string, 0, len(subs))
	for _, s := range subs {
		names = append(names, s.Name)
	}
	return names
}

func optionLabels(options []Option) []string {
	var labels []string
	for _, o := range options {
		labels = append(labels, strings.Join(o.Names, ","))
	}
	return labels
}

func TestRenderCollectsSubcommands(t *testing.T) {
	t.Parallel()

	page, err := Render([]byte(toolPage))
	if err != nil {
		t.Fatal(err)
	}
	if got := subcommandNames(page.subcommands); !slices.Equal(got, []string{"start", "stop", "sync", "sync-all"}) {
		t.Errorf("subcommands = %v", got)
	}
	start, _ := page.subcommand("start")
	if start.Summary != "Start one or more units." || start.Doc != "```\ntool start UNIT...\n```\n\nStart one or more units. Units are started in parallel." {
		t.Errorf("start = %+v", start)
	}
	if !slices.Equal(optionLabels(start.Options), []string{"--force", "-v,--verbose"}) {
		t.Errorf("start options = %v, want the parent page's options", optionLabels(start.Options))
	}
	if stop, _ := page.subcommand("stop"); !slices.Equal(optionLabels(stop.Options), []string{"--force"}) {
		t.Errorf("stop options = %v, want only the options nested under its entry", optionLabels(stop.Options))
	}
	if all, _ := page.subcommand("sync-all"); all.Doc != "```\ntool sync-all\n```\n\nSynchronise everything." {
		t.Errorf("sync-all = %+v", all)
	}
	if !slices.Equal(page.Description, []string{"Run this:"}) {
		t.Errorf("Description = %q, want the lead-in kept as prose", page.Description)
	}
}

func TestRenderCollectsMdocSubcommands(t *testing.T) {
	t.Parallel()

	page, err := Render([]byte(archivePage))
	if err != nil {
		t.Fatal(err)
	}
	if got := subcommandNames(page.subcommands); !slices.Equal(got, []string{"add", "list"}) {
		t.Fatalf("subcommands = %v", got)
	}
	add, _ := page.subcommand("add")
	if add.Summary != "Add files to the archive." || !strings.Contains(add.Doc, "archive add [-f] file ...") {
		t.Errorf("add = %+v", add)
	}
	if !slices.Equal(optionLabels(add.Options), []string{"-f"}) {
		t.Errorf("add options = %v", optionLabels(add.Options))
	}
}

func TestRenderIgnoresCommandsTheSynopsisDoesNotTake(t *testing.T) {
	t.Parallel()

	page, err := Render([]byte(pagerPage))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.subcommands) != 0 {
		t.Errorf("interactive commands taken as subcommands: %v", subcommandNames(page.subcommands))
	}
}

func toolLookup(t *testing.T) *Lookup {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "man1", "tool.1"), []byte(toolPage))
	writeFile(t, filepath.Join(dir, "man1", "tool-start.1.gz"), gzipped(t, toolStartPage))
	writeFile(t, filepath.Join(dir, "man1", "tool-extra.1"), []byte(toolExtraPage))
	writeFile(t, filepath.Join(dir, "man1", "tool-helper.1"), []byte(toolHelperPage))
	writeFile(t, filepath.Join(dir, "man1", "tool-Mixed.1"), []byte(toolExtraPage))
	return NewLookup(dir, "")
}

func TestLookupListsSubcommandsLazily(t *testing.T) {
	t.Parallel()

	l := toolLookup(t)
	listing, err := l.Subcommands(t.Context(), "tool")
	if err != nil || len(listing.Subcommands) != 0 || !slices.Equal(listing.Unloaded, []string{"tool"}) {
		t.Fatalf("Subcommands before loading = %+v, %v; want the parent page requested", listing, err)
	}
	if _, err := l.Page(t.Context(), "tool"); err != nil {
		t.Fatal(err)
	}
	listing, err = l.Subcommands(t.Context(), "tool")
	if err != nil || !slices.Equal(listing.Unloaded, []string{"tool-extra", "tool-helper"}) {
		t.Fatalf("Subcommands = %+v, %v; want the unlisted pages requested", listing, err)
	}
	for _, name := range listing.Unloaded {
		_, _ = l.Page(t.Context(), name)
	}
	listing, err = l.Subcommands(t.Context(), "tool")
	if err != nil || len(listing.Unloaded) != 0 {
		t.Fatalf("Subcommands after loading = %+v, %v", listing, err)
	}
	if got := subcommandNames(listing.Subcommands); !slices.Equal(got, []string{"extra", "start", "stop", "sync", "sync-all"}) {
		t.Errorf("subcommands = %v, want tool-helper rejected for not invoking tool", got)
	}
	extra := listing.Subcommands[0]
	if extra.Summary != "extra widget tricks" || !strings.HasPrefix(extra.Doc, "**tool-extra - extra widget tricks**") {
		t.Errorf("extra = %+v", extra)
	}
	if listing, _ := l.Subcommands(t.Context(), "nonexistent"); len(listing.Subcommands)+len(listing.Unloaded) != 0 {
		t.Errorf("Subcommands(nonexistent) = %+v", listing)
	}
}

func TestLookupResolvesSubcommands(t *testing.T) {
	t.Parallel()

	l := toolLookup(t)
	start, err := l.Subcommand(t.Context(), "tool", "start")
	if err != nil {
		t.Fatal(err)
	}
	if start.Summary != "start widget units" || !strings.Contains(start.Doc, "tool start [-w] UNIT...") || !slices.Equal(optionLabels(start.Options), []string{"-w,--wait"}) {
		t.Errorf("start = %+v, want its own page", start)
	}
	stop, err := l.Subcommand(t.Context(), "tool", "stop")
	if err != nil || stop.Summary != "Stop units." || !slices.Equal(optionLabels(stop.Options), []string{"--force"}) {
		t.Errorf("stop = %+v, %v; want the parent's entry", stop, err)
	}
	extra, err := l.Subcommand(t.Context(), "tool", "extra")
	if err != nil || !slices.Equal(optionLabels(extra.Options), []string{"-x"}) {
		t.Errorf("extra = %+v, %v; want the verified unlisted page", extra, err)
	}
	for _, name := range []string{"helper", "Mixed", "missing", "-v", ""} {
		if _, err := l.Subcommand(t.Context(), "tool", name); !errors.Is(err, ErrNoPage) {
			t.Errorf("Subcommand(tool, %q) error = %v, want ErrNoPage", name, err)
		}
	}
}

func TestLookupOptionsFollowTheSubcommand(t *testing.T) {
	t.Parallel()

	l := toolLookup(t)
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{nil, []string{"--force", "-v,--verbose"}},
		{[]string{"-v", "stop"}, []string{"--force"}},
		{[]string{"start", "web"}, []string{"-w,--wait"}},
		{[]string{"sync"}, []string{"--force", "-v,--verbose"}},
		{[]string{"web"}, []string{"--force", "-v,--verbose"}},
	} {
		options, err := l.Options(t.Context(), "tool", tc.args)
		if err != nil || !slices.Equal(optionLabels(options), tc.want) {
			t.Errorf("Options(tool, %q) = %v, %v; want %v", tc.args, optionLabels(options), err, tc.want)
		}
	}
	if _, err := l.Options(t.Context(), "missing", []string{"start"}); !errors.Is(err, ErrNoPage) {
		t.Errorf("Options(missing) error = %v, want ErrNoPage", err)
	}
}

func TestSubcommandWord(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args []string
		want string
		ok   bool
	}{
		{nil, "", false},
		{[]string{"-v", "--all"}, "", false},
		{[]string{"-v", "checkout", "-b"}, "checkout", true},
	} {
		if got, ok := SubcommandWord(tc.args); got != tc.want || ok != tc.ok {
			t.Errorf("SubcommandWord(%q) = %q, %t", tc.args, got, ok)
		}
	}
}

func TestRealSubcommands(t *testing.T) {
	t.Parallel()

	l := NewLookup(os.Getenv("MANPATH"), os.Getenv("PATH"))
	if !l.Has("systemctl") {
		t.Skip("no systemctl manual page installed")
	}
	if _, err := l.Page(t.Context(), "systemctl"); err != nil {
		t.Fatal(err)
	}
	listing, err := l.Subcommands(t.Context(), "systemctl")
	if err != nil {
		t.Fatal(err)
	}
	names := subcommandNames(listing.Subcommands)
	for _, want := range []string{"start", "restart", "list-units", "daemon-reload"} {
		if !slices.Contains(names, want) {
			t.Errorf("systemctl subcommands lack %s: %v", want, names)
		}
	}
	if slices.Contains(names, "true") {
		t.Errorf("an option value became a subcommand: %v", names)
	}
}

func TestRealSubcommandPage(t *testing.T) {
	t.Parallel()

	l := NewLookup(os.Getenv("MANPATH"), os.Getenv("PATH"))
	if !l.Has("git-checkout") {
		t.Skip("no git-checkout manual page installed")
	}
	checkout, err := l.Subcommand(t.Context(), "git", "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(checkout.Options, func(o Option) bool { return slices.Contains(o.Names, "-b") }) {
		t.Errorf("git checkout options lack -b: %v", optionLabels(checkout.Options))
	}
}
