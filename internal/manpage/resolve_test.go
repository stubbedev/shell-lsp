package manpage

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const vcsPage = `.TH VCS 1
.SH NAME
vcs \- track versions of files
.SH SYNOPSIS
.nf
\fBvcs\fR [\fB\-C\fR \fI<path>\fR] [\fB\-\-git\-dir=\fR\fI<path>\fR] [\fB\-p\fR | \fB\-\-paginate\fR] \fI<command>\fR [\fI<args>\fR]
.fi
.SH OPTIONS
.PP
\fB\-C\fR \fI<path>\fR
.RS 4
Run as if started in path\&.
.RE
.PP
\fB\-\-git\-dir=\fR\fI<path>\fR
.RS 4
Set the repository\&.
.RE
.PP
\fB\-p\fR, \fB\-\-paginate\fR
.RS 4
Page the output\&.
.RE
.PP
\fB\-\-no\-pager\fR
.RS 4
Do not page\&.
.RE
.SH "MAIN COMMANDS"
.PP
\fBvcs-remote\fR(1)
.RS 4
Manage remotes\&.
.RE
.PP
\fBvcs-stash\fR(1)
.RS 4
Stash changes away\&.
.RE
`

const vcsRemotePage = `.TH VCS-REMOTE 1
.SH NAME
vcs-remote \- manage the set of tracked repositories
.SH SYNOPSIS
.nf
\fBvcs\fR \fBremote\fR [\fB\-v\fR | \fB\-\-verbose\fR]
\fBvcs\fR \fBremote\fR \fBadd\fR [\fB\-t\fR \fI<branch>\fR] [\fB\-f\fR] \fI<name>\fR \fI<URL>\fR
\fBvcs\fR \fBremote\fR \fBrename\fR \fI<old>\fR \fI<new>\fR
.fi
.SH COMMANDS
.PP
\fBadd\fR
.RS 4
Add a remote named \fI<name>\fR\&.
.PP
\fB\-t\fR \fI<branch>\fR
.RS 4
Track only branch\&.
.RE
.PP
\fB\-f\fR
.RS 4
Fetch right away\&.
.RE
.RE
.PP
\fBrename\fR
.RS 4
Rename a remote\&.
.RE
.SH OPTIONS
.PP
\fB\-v\fR, \fB\-\-verbose\fR
.RS 4
Be verbose\&.
.RE
`

const vcsStashPage = `.TH VCS-STASH 1
.SH NAME
vcs-stash \- stash the changes in a dirty working directory away
.SH SYNOPSIS
.nf
\fBvcs\fR \fBstash\fR \fBlist\fR
\fBvcs\fR \fBstash\fR [\fBpush\fR] [\fB\-m\fR \fI<message>\fR]
.fi
.SH COMMANDS
.PP
\fBpush\fR
.RS 4
Save local modifications\&.
.RE
.PP
\fBlist\fR
.RS 4
List the stash entries\&.
.RE
.SH OPTIONS
.PP
\fB\-m\fR \fI<message>\fR
.RS 4
Describe the stash\&.
.RE
`

const kubePage = `.nh
.TH "KUBE" "1" "Jan 2026" "" ""

.SH NAME
kube \- controls the cluster manager

.SH SYNOPSIS
\fBkube\fP [flags]

.SH OPTIONS
\fB-n\fP, \fB--namespace\fP=""
	If present, the namespace scope for this CLI request

.PP
\fB-v\fP, \fB--v\fP=0
	number for the log level verbosity

.PP
\fB--warnings-as-errors\fP[=false]
	Treat warnings received from the server as errors

.SH SEE ALSO
\fBkube-config(1)\fP, \fBkube-get(1)\fP
`

const kubeConfigPage = `.nh
.TH "KUBE" "1" "Jan 2026" "" ""

.SH NAME
kube-config \- Modify kubeconfig files

.SH SYNOPSIS
\fBkube config SUBCOMMAND\fP

.SH OPTIONS INHERITED FROM PARENT COMMANDS
\fB-n\fP, \fB--namespace\fP=""
	If present, the namespace scope for this CLI request
`

const kubeSetContextPage = `.nh
.TH "KUBE" "1" "Jan 2026" "" ""

.SH NAME
kube-config-set-context \- Set a context entry in kubeconfig

.SH SYNOPSIS
\fBkube config set-context\fP [NAME | --current] [flags]

.SH OPTIONS
\fB--cluster\fP=""
	cluster for the context entry in kubeconfig

.PP
\fB--current\fP[=false]
	Modify the current context
`

const kubeGetPage = `.nh
.TH "KUBE" "1" "Jan 2026" "" ""

.SH NAME
kube-get \- Display one or many resources

.SH SYNOPSIS
\fBkube get\fP [(-o|--output=)json] (TYPE[.VERSION][.GROUP] [NAME | -l label] | TYPE[.VERSION][.GROUP]/NAME ...) [flags]
`

const netPage = `.TH NET 8
.SH NAME
net \- configure network objects
.SH SYNOPSIS
.B net
.RI "[ " OPTIONS " ] " OBJECT " { " COMMAND " | "
.BR help " }"
.SH OPTIONS
.TP
.BR "\-n" , " \-netns " <NETNS>
switch to the network namespace
.SH "NET - COMMAND SYNTAX"
.TP
.B link
\- network device.
`

const netLinkPage = `.TH NET\-LINK 8
.SH NAME
net-link \- network device configuration
.SH SYNOPSIS
.B net link
.RI " { " COMMAND " | "
.BR help " }"
.sp
.BI "net link set " DEVICE
.RB "[ " up " | " down " ]"
.SH DESCRIPTION
.SS net link add - add virtual link
.TP
.BI type " TYPE"
the link type
.SS net link set - change device attributes
.TP
.BI dev " DEVICE"
the device to change
.SS
net link show - display device attributes
.SH EXAMPLES
.SS Examples of usage
.PP
net link show
`

func writePages(t *testing.T, section string, pages map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range pages {
		writeFile(t, filepath.Join(dir, "man"+section, name+"."+section), []byte(src))
	}
	return dir
}

func nestedLookup(t *testing.T) *Lookup {
	t.Helper()
	dir := writePages(t, "1", map[string]string{
		"vcs": vcsPage, "vcs-remote": vcsRemotePage, "vcs-stash": vcsStashPage,
		"kube": kubePage, "kube-config": kubeConfigPage, "kube-config-set-context": kubeSetContextPage, "kube-get": kubeGetPage,
	})
	for name, src := range map[string]string{"net": netPage, "net-link": netLinkPage} {
		writeFile(t, filepath.Join(dir, "man8", name+".8"), []byte(src))
	}
	return NewLookup(dir, "")
}

func loadAll(t *testing.T, l *Lookup) {
	t.Helper()
	for name := range l.library.index() {
		if _, err := l.Page(t.Context(), name); err != nil {
			t.Fatalf("Page(%s) error = %v", name, err)
		}
	}
}

func TestLookupListsNestedSubcommands(t *testing.T) {
	t.Parallel()

	l := nestedLookup(t)
	loadAll(t, l)
	for _, tc := range []struct {
		command string
		args    []string
		want    []string
	}{
		{"vcs", nil, []string{"remote", "stash"}},
		{"vcs", []string{"-C", "dir"}, []string{"remote", "stash"}},
		{"vcs", []string{"-Cdir", "--no-pager"}, []string{"remote", "stash"}},
		{"vcs", []string{"-C"}, nil},
		{"vcs", []string{"remote"}, []string{"add", "rename"}},
		{"vcs", []string{"-C", "dir", "--git-dir", "x", "-p", "remote", "-v"}, []string{"add", "rename"}},
		{"vcs", []string{"--git-dir=x", "remote"}, []string{"add", "rename"}},
		{"vcs", []string{"-pC", "dir", "remote"}, []string{"add", "rename"}},
		{"vcs", []string{"-C", "remote", "stash"}, []string{"list", "push"}},
		{"vcs", []string{"--unknown", "remote"}, []string{"add", "rename"}},
		{"vcs", []string{"--unknown", "dir", "remote"}, []string{"add", "rename"}},
		{"vcs", []string{"--unknown", "dir"}, nil},
		{"vcs", []string{"remote", "add"}, nil},
		{"vcs", []string{"remote", "origin"}, nil},
		{"vcs", []string{"--", "remote"}, nil},
		{"kube", nil, []string{"config", "get"}},
		{"kube", []string{"-n", "ns", "config"}, []string{"set-context"}},
		{"kube", []string{"--namespace", "config", "config"}, []string{"set-context"}},
		{"kube", []string{"--warnings-as-errors", "config"}, []string{"set-context"}},
		{"net", []string{"-netns", "blue", "link"}, []string{"add", "set", "show"}},
	} {
		listing, err := l.Subcommands(t.Context(), tc.command, tc.args)
		if err != nil || len(listing.Unloaded) != 0 || !slices.Equal(subcommandNames(listing.Subcommands), tc.want) {
			t.Errorf("Subcommands(%s %q) = %v (unloaded %v), %v; want %v", tc.command, tc.args, subcommandNames(listing.Subcommands), listing.Unloaded, err, tc.want)
		}
	}
}

func TestLookupResolvesNestedSubcommands(t *testing.T) {
	t.Parallel()

	l := nestedLookup(t)
	add, err := l.Subcommand(t.Context(), "vcs", []string{"-C", "dir", "remote", "add"})
	if err != nil {
		t.Fatal(err)
	}
	if add.Summary != "Add a remote named <name>." || !strings.HasPrefix(add.Doc, "```\nvcs remote add\n```") {
		t.Errorf("remote add = %+v", add)
	}
	if !slices.Equal(optionLabels(add.Options), []string{"-t", "-f"}) {
		t.Errorf("remote add options = %v", optionLabels(add.Options))
	}
	set, err := l.Subcommand(t.Context(), "kube", []string{"-n", "ns", "config", "set-context"})
	if err != nil || set.Summary != "Set a context entry in kubeconfig" {
		t.Fatalf("config set-context = %+v, %v", set, err)
	}
	valued := map[string]bool{}
	for _, o := range set.Options {
		valued[strings.Join(o.Names, ",")] = o.Valued
	}
	if len(valued) != 2 || !valued["--cluster"] || valued["--current"] {
		t.Errorf("config set-context options = %v", valued)
	}
	link, err := l.Subcommand(t.Context(), "net", []string{"link", "set"})
	if err != nil || link.Summary != "change device attributes" || !strings.HasPrefix(link.Doc, "```\nnet link set\n```") {
		t.Errorf("net link set = %+v, %v", link, err)
	}
	for _, args := range [][]string{{"-C", "dir"}, {"-C", "remote"}, {"remote", "origin"}, {"remote", "add", "origin"}, nil} {
		if _, err := l.Subcommand(t.Context(), "vcs", args); !errors.Is(err, ErrNoPage) {
			t.Errorf("Subcommand(vcs %q) error = %v, want ErrNoPage", args, err)
		}
	}
}

func TestLookupOptionsOfTheDeepestSubcommand(t *testing.T) {
	t.Parallel()

	l := nestedLookup(t)
	for _, tc := range []struct {
		command string
		args    []string
		want    []string
	}{
		{"vcs", []string{"-C", "dir"}, []string{"-C", "--git-dir", "-p,--paginate", "--no-pager"}},
		{"vcs", []string{"-C", "dir", "remote"}, []string{"-t", "-f", "-v,--verbose"}},
		{"vcs", []string{"remote", "-v", "add", "origin"}, []string{"-t", "-f"}},
		{"vcs", []string{"remote", "rename"}, []string{"-t", "-f", "-v,--verbose"}},
		{"vcs", []string{"stash", "push", "-m", "msg"}, []string{"-m"}},
		{"kube", []string{"config"}, []string{"-n,--namespace"}},
	} {
		options, err := l.Options(t.Context(), tc.command, tc.args)
		if err != nil || !slices.Equal(optionLabels(options), tc.want) {
			t.Errorf("Options(%s %q) = %v, %v; want %v", tc.command, tc.args, optionLabels(options), err, tc.want)
		}
	}
}

func TestLookupResolvesLazily(t *testing.T) {
	t.Parallel()

	l := nestedLookup(t)
	for _, tc := range []struct {
		command string
		args    []string
		want    []string
	}{
		{"vcs", []string{"-C", "dir", "remote", "add"}, []string{"vcs", "vcs-remote"}},
		{"kube", []string{"-n", "ns", "config", "set-context", "prod"}, []string{"kube", "kube-config", "kube-config-set-context"}},
		{"missing", []string{"x"}, nil},
	} {
		unloaded, err := l.Unloaded(tc.command, tc.args)
		if err != nil || !slices.Equal(unloaded, tc.want) {
			t.Errorf("Unloaded(%s %q) = %v, %v; want %v", tc.command, tc.args, unloaded, err, tc.want)
		}
	}
	listing, err := l.Subcommands(t.Context(), "vcs", []string{"remote"})
	if err != nil || len(listing.Subcommands) != 0 || !slices.Equal(listing.Unloaded, []string{"vcs", "vcs-remote"}) {
		t.Fatalf("Subcommands before loading = %+v, %v", listing, err)
	}
	if _, err := l.Page(t.Context(), "vcs"); err != nil {
		t.Fatal(err)
	}
	listing, err = l.Subcommands(t.Context(), "vcs", []string{"remote"})
	if err != nil || len(listing.Subcommands) != 0 || !slices.Equal(listing.Unloaded, []string{"vcs-remote"}) {
		t.Fatalf("Subcommands with the parent loaded = %+v, %v", listing, err)
	}
	if _, err := l.Page(t.Context(), "vcs-remote"); err != nil {
		t.Fatal(err)
	}
	listing, err = l.Subcommands(t.Context(), "vcs", []string{"remote"})
	if err != nil || len(listing.Unloaded) != 0 || !slices.Equal(subcommandNames(listing.Subcommands), []string{"add", "rename"}) {
		t.Errorf("Subcommands after loading = %+v, %v", listing, err)
	}
	if cached, _ := l.cached("vcs-stash"); cached.err != nil || cached.page.Summary != "" {
		t.Errorf("listing vcs remote loaded unrelated pages: %+v", cached)
	}
}

func TestLookupReportsUnreadableSubcommandPages(t *testing.T) {
	t.Parallel()

	dir := writePages(t, "1", map[string]string{"vcs": vcsPage})
	broken := filepath.Join(dir, "man1", "vcs-remote.1.gz")
	writeFile(t, broken, []byte("not gzip"))
	l := NewLookup(dir, "")
	if _, err := l.Options(t.Context(), "vcs", []string{"remote", "add"}); err == nil || errors.Is(err, ErrNoPage) {
		t.Errorf("Options() error = %v, want the unreadable page reported", err)
	}
	if _, err := l.Subcommands(t.Context(), "vcs", []string{"remote"}); err == nil || errors.Is(err, ErrNoPage) {
		t.Errorf("Subcommands() error = %v, want the unreadable page reported", err)
	}
	if _, err := os.Stat(broken); err != nil {
		t.Fatal(err)
	}
}

func TestRealNestedSubcommands(t *testing.T) {
	t.Parallel()

	l := NewLookup(os.Getenv("MANPATH"), os.Getenv("PATH"))
	if !l.Has("git") || !l.Has("git-remote") {
		t.Skip("no git and git-remote manual pages installed")
	}
	add, err := l.Subcommand(t.Context(), "git", []string{"-C", "repo", "--no-pager", "remote", "add"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(add.Doc, "git remote add") {
		t.Errorf("git remote add = %+v", add)
	}
	listing, err := l.Subcommands(t.Context(), "git", []string{"-C", "repo", "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if names := subcommandNames(listing.Subcommands); !slices.Contains(names, "add") || !slices.Contains(names, "set-url") {
		t.Errorf("git remote subcommands = %v", names)
	}
}

func TestRealIpLinkSubcommands(t *testing.T) {
	t.Parallel()

	l := NewLookup(os.Getenv("MANPATH"), os.Getenv("PATH"))
	if !l.Has("ip") || !l.Has("ip-link") {
		t.Skip("no ip and ip-link manual pages installed")
	}
	set, err := l.Subcommand(t.Context(), "ip", []string{"-n", "blue", "link", "set"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(set.Doc, "ip link set") {
		t.Errorf("ip link set = %+v", set)
	}
}

func TestRealSystemctlStaysOneLevel(t *testing.T) {
	t.Parallel()

	l := NewLookup(os.Getenv("MANPATH"), os.Getenv("PATH"))
	if !l.Has("systemctl") {
		t.Skip("no systemctl manual page installed")
	}
	if _, err := l.Page(t.Context(), "systemctl"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"start"}, {"--user", "list-units"}, {"-t", "service", "show"}} {
		listing, err := l.Subcommands(t.Context(), "systemctl", args)
		if err != nil || len(listing.Subcommands) != 0 {
			t.Errorf("Subcommands(systemctl %q) = %v, %v; want none", args, subcommandNames(listing.Subcommands), err)
		}
		if _, err := l.Subcommand(t.Context(), "systemctl", args); err != nil {
			t.Errorf("Subcommand(systemctl %q) error = %v", args, err)
		}
	}
}
