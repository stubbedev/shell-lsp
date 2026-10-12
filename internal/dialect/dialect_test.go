package dialect

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func TestDetectPrecedence(t *testing.T) {
	t.Parallel()

	zsh := Zsh
	tests := []struct {
		name     string
		override *Dialect
		in       Input
		want     Dialect
	}{
		{
			name: "override beats everything", override: &zsh,
			in:   Input{URI: "file:///w/a.bash", LanguageID: "bash", Text: "#!/bin/sh\n"},
			want: Zsh,
		},
		{name: "shebang bash", in: Input{Text: "#!/bin/bash\necho hi\n"}, want: Bash},
		{name: "shebang with flags", in: Input{Text: "#!/bin/bash -euo pipefail\necho hi\n"}, want: Bash},
		{name: "shebang env zsh", in: Input{Text: "#!/usr/bin/env zsh\n"}, want: Zsh},
		{name: "shebang env with flags", in: Input{Text: "#!/usr/bin/env -S bash -e\n"}, want: Bash},
		{name: "shebang absolute custom prefix", in: Input{Text: "#!/nix/store/...-bash-5.2/bin/bash\n"}, want: Bash},
		{name: "shebang sh", in: Input{Text: "#!/bin/sh\n"}, want: Posix},
		{name: "shebang dash", in: Input{Text: "#!/usr/bin/dash\n"}, want: Dash},
		{name: "shebang ksh93", in: Input{Text: "#!/bin/ksh93\n"}, want: Ksh},
		{name: "shebang mksh is ksh", in: Input{Text: "#!/bin/mksh\n"}, want: Ksh},
		{name: "completion function", in: Input{URI: "file:///w/_deploy", Text: "#compdef deploy\n_arguments '1: :->cmd'\n"}, want: Zsh},
		{name: "completion helper", in: Input{URI: "file:///w/_deploy_hosts", Text: "#autoload\n"}, want: Zsh},
		{name: "completion tag beats extension", in: Input{URI: "file:///w/_tool.sh", Text: "#compdef tool -p 'tool-*'\n"}, want: Zsh},
		{name: "completion tag only on the first line", in: Input{URI: "file:///w/untitled", Text: "#!/bin/bash\n#compdef deploy\n"}, want: Bash},
		{name: "tag needs to be the whole first word", in: Input{URI: "file:///w/untitled", Text: "#compdefault\n"}, want: Bash},
		{name: "shebang beats extension", in: Input{URI: "file:///w/a.zsh", Text: "#!/bin/bash\n"}, want: Bash},
		{name: "no shebang, extension bash", in: Input{URI: "file:///w/deploy.bash"}, want: Bash},
		{name: "bashrc by name", in: Input{URI: "file:///home/u/.bashrc"}, want: Bash},
		{name: "zshrc by name", in: Input{URI: "file:///home/u/.zshrc"}, want: Zsh},
		{name: "extension zsh", in: Input{URI: "file:///w/setup.zsh"}, want: Zsh},
		{name: "extension sh is posix", in: Input{URI: "file:///w/lib.sh"}, want: Posix},
		{name: "extension ksh", in: Input{URI: "file:///w/old.ksh"}, want: Ksh},
		{name: "extension beats languageId", in: Input{URI: "file:///w/x.sh", LanguageID: "zsh"}, want: Posix},
		{name: "languageId bash", in: Input{URI: "file:///w/untitled", LanguageID: "bash"}, want: Bash},
		{name: "languageId shellscript", in: Input{URI: "file:///w/untitled", LanguageID: "shellscript"}, want: Bash},
		{name: "languageId unknown falls through to shellcheck", in: Input{
			URI: "file:///w/untitled", LanguageID: "plaintext",
			Text: "# shellcheck shell=dash\n",
		}, want: Dash},
		{name: "shellcheck directive beats shebang", in: Input{
			URI: "file:///w/x.sh", LanguageID: "sh",
			Text: "#!/bin/sh\n# shellcheck shell=ksh\n",
		}, want: Ksh},
		{name: "shellcheck directive after the first command is ignored", in: Input{
			Text: "#!/bin/sh\n:\n# shellcheck shell=bash\n",
		}, want: Posix},
		{name: "first shellcheck directive wins", in: Input{
			Text: "\n# shellcheck shell=sh\n# shellcheck shell=bash\n",
		}, want: Posix},
		{name: "unknown shellcheck shell means bash", in: Input{
			Text: "#!/bin/sh\n# shellcheck shell=fish\n",
		}, want: Bash},
		{name: "quoted busybox directive", in: Input{
			Text: "# shellcheck shell='busybox sh'\n",
		}, want: Busybox},
		{name: "shebang busybox ash", in: Input{Text: "#!/bin/busybox ash\n"}, want: Busybox},
		{name: "shebang env busybox sh", in: Input{Text: "#!/usr/bin/env busybox sh\n"}, want: Busybox},
		{name: "modeline last resort", in: Input{
			URI: "file:///w/untitled", LanguageID: "plaintext",
			Text: "# vim: set ft=zsh:\n",
		}, want: Zsh},
		{name: "modeline filetype spelling", in: Input{
			URI: "file:///w/untitled", LanguageID: "plaintext",
			Text: "# vi: filetype=sh\n",
		}, want: Posix},
		{name: "default is bash", in: Input{URI: "file:///w/untitled", LanguageID: "plaintext"}, want: Bash},
		{name: "crlf shebang", in: Input{Text: "#!/usr/bin/env bash\r\necho hi\r\n"}, want: Bash},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Detect(tc.override, tc.in); got != tc.want {
				t.Errorf("Detect() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestOverrideNilMeansDetect(t *testing.T) {
	t.Parallel()

	if got := Detect(nil, Input{Text: "#!/bin/zsh\n"}); got != Zsh {
		t.Errorf("Detect(nil override) = %s, want zsh", got)
	}
}

func TestResolveReportsWhatDecided(t *testing.T) {
	t.Parallel()

	ksh := Ksh
	for _, c := range []struct {
		override *Dialect
		in       Input
		want     Detection
		guessed  bool
	}{
		{nil, Input{Text: "echo\n"}, Detection{Dialect: Bash, Source: SourceDefault}, true},
		{nil, Input{URI: "file:///w/run", Text: "echo\n"}, Detection{Dialect: Bash, Source: SourceDefault}, true},
		{nil, Input{Text: "#!/usr/bin/env -S sh -e\r\necho\n"}, Detection{Dialect: Posix, Source: SourceShebang, Evidence: "#!/usr/bin/env -S sh -e"}, false},
		{nil, Input{Text: "#!/usr/bin/python3\n", URI: "file:///w/a.sh"}, Detection{Dialect: Posix, Source: SourceExtension, Evidence: ".sh"}, true},
		{nil, Input{Text: "# shellcheck shell=fish\necho\n"}, Detection{Dialect: Bash, Source: SourceDirective, Evidence: "fish"}, false},
		{nil, Input{Text: "#!/bin/bash\n# shellcheck shell=dash\necho\n"}, Detection{Dialect: Dash, Source: SourceDirective, Evidence: "dash"}, false},
		{nil, Input{Text: "#compdef git\n"}, Detection{Dialect: Zsh, Source: SourceCompletionTag, Evidence: "#compdef"}, false},
		{nil, Input{URI: "file:///w/a.zsh", Text: "echo\n"}, Detection{Dialect: Zsh, Source: SourceExtension, Evidence: ".zsh"}, true},
		{nil, Input{URI: "file:///home/u/.zshrc", Text: "echo\n"}, Detection{Dialect: Zsh, Source: SourceFileName, Evidence: ".zshrc"}, false},
		{nil, Input{URI: "file:///home/u/.bash_local", Text: "echo\n"}, Detection{Dialect: Bash, Source: SourceFileName, Evidence: ".bash_local"}, false},
		{nil, Input{LanguageID: "bash", Text: "echo\n"}, Detection{Dialect: Bash, Source: SourceLanguageID, Evidence: "bash"}, false},
		{nil, Input{Text: "echo\n# vim: set ft=zsh:\n"}, Detection{Dialect: Zsh, Source: SourceModeline, Evidence: "zsh"}, false},
		{nil, Input{LanguageID: "shellscript", Text: "echo\n# vim: set ft=zsh:\n"}, Detection{Dialect: Zsh, Source: SourceModeline, Evidence: "zsh"}, false},
		{nil, Input{LanguageID: "shellscript", Text: "echo\n"}, Detection{Dialect: Bash, Source: SourceDefault}, true},
		{&ksh, Input{Text: "#!/bin/bash\necho\n"}, Detection{Dialect: Ksh, Source: SourceOverride, Evidence: "ksh"}, false},
	} {
		got := Resolve(c.override, c.in)
		if got != c.want || got.Guessed() != c.guessed || got.Named() != (c.want.Source != SourceDefault) {
			t.Errorf("Resolve(%v, %+v) = %+v (guessed %t); want %+v (guessed %t)", c.override, c.in, got, got.Guessed(), c.want, c.guessed)
		}
	}
}

func TestDescribeSaysWhyAndWhenItGuessed(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		d    Detection
		want string
	}{
		{Detection{Dialect: Bash, Source: SourceShebang, Evidence: "#!/usr/bin/env bash"}, "from the shebang #!/usr/bin/env bash"},
		{Detection{Dialect: Posix, Source: SourceExtension, Evidence: ".sh"}, "guessed from the .sh extension"},
		{Detection{Dialect: Bash, Source: SourceDefault}, "guessed: nothing names the shell, so bash is assumed"},
		{Detection{Dialect: Bash, Source: SourceDirective, Evidence: "fish"}, "from the directive # shellcheck shell=fish; fish is not a shell shell-lsp knows, so it is checked as bash"},
		{Detection{Dialect: Posix, Source: SourceDirective, Evidence: "posix"}, "from the directive # shellcheck shell=posix"},
		{Detection{Dialect: Posix, Source: SourceEditorConfig, Evidence: "posix"}, "from shell_variant = posix in .editorconfig"},
		{Detection{Dialect: Bash, Source: SourceHostSetting, Evidence: "SHELL := /bin/bash"}, "from SHELL := /bin/bash"},
		{Detection{Dialect: Posix, Source: SourceHostDefault, Evidence: "make"}, "the shell make runs scripts with unless told otherwise"},
	} {
		if got := c.d.Describe(); got != c.want {
			t.Errorf("%+v.Describe() = %q, want %q", c.d, got, c.want)
		}
		if host := c.d.Source == SourceHostSetting || c.d.Source == SourceHostDefault; host && c.d.Guessed() {
			t.Errorf("%+v counts as a guess", c.d)
		}
	}
	for s := range Source(sourceCount) {
		if s.String() == "unknown" {
			t.Errorf("source %d has no name", s)
		}
	}
}

func TestFromNameTable(t *testing.T) {
	t.Parallel()

	tests := map[string]Dialect{
		"bash": Bash, "BASH": Bash, "shellscript": Bash,
		"zsh": Zsh, "sh": Posix, "posix": Posix,
		"dash": Dash, "ash": Dash, "ksh": Ksh, "ksh93": Ksh, "mksh": Ksh,
		"bats": Bash, "ksh88": Ksh, "oksh": Ksh,
		"busybox": Busybox, "busybox sh": Busybox, "busybox ash": Busybox,
	}
	for name, want := range tests {
		got, ok := FromName(name)
		if !ok || got != want {
			t.Errorf("FromName(%q) = %v, %v; want %v", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "python", "fish", "powershell"} {
		if _, ok := FromName(name); ok {
			t.Errorf("FromName(%q) matched, want no match", name)
		}
	}
}

func TestFeaturesDiffer(t *testing.T) {
	t.Parallel()

	if Posix.Features().DoubleBracket {
		t.Error("posix claims [[ ]] support")
	}
	if !Bash.Features().DoubleBracket || !Bash.Features().AssociativeArrays {
		t.Error("bash missing [[ ]] or associative arrays")
	}
	if !Zsh.Features().GlobQualifiers || Bash.Features().GlobQualifiers {
		t.Error("glob qualifiers misattributed")
	}
	for _, d := range []Dialect{Bash, Posix, Dash, Ksh, Busybox} {
		f := d.Features()
		if !f.GlobsUnquoted || !f.SplitsUnquoted || !f.QuotedRegexLiteral || f.ArithmeticStatus || f.AssignsPositionals || f.AssociativePairs {
			t.Errorf("%s claims zsh's expansion, status or assignment rules", d)
		}
	}
	if z := Zsh.Features(); z.GlobsUnquoted || z.SplitsUnquoted || z.QuotedRegexLiteral || !z.ArithmeticStatus || !z.AssignsPositionals || !z.AssociativePairs {
		t.Error("zsh expansion, status or assignment rules misattributed")
	}
	if !Dash.Features().Local {
		t.Error("dash local support missing")
	}
	if Posix.Features().Local {
		t.Error("posix sh claims local support")
	}
	if !Busybox.Features().Local || !Busybox.Features().DoubleBracket || Busybox.Features().Arrays {
		t.Error("busybox sh features misattributed")
	}
}

func TestSet(t *testing.T) {
	t.Parallel()

	s := SetOf(Bash, Ksh)
	if !s.Has(Bash) || !s.Has(Ksh) || s.Has(Zsh) || s.Has(Posix) || s.Has(Dash) {
		t.Errorf("SetOf(Bash, Ksh) membership wrong: %08b", s)
	}
	every := []Dialect{Bash, Zsh, Posix, Dash, Ksh, Busybox}
	for _, d := range every {
		if !All.Has(d) {
			t.Errorf("All lacks %s", d)
		}
	}
	if got := slices.Collect(All.Members()); !slices.Equal(got, every) {
		t.Errorf("All.Members() = %v, want %v", got, every)
	}
	if got := slices.Collect(s.Members()); !slices.Equal(got, []Dialect{Bash, Ksh}) {
		t.Errorf("SetOf(Bash, Ksh).Members() = %v", got)
	}
}

func TestEveryDialectIsDescribedOnce(t *testing.T) {
	t.Parallel()

	if got, want := Names(), []string{"bash", "zsh", "sh", "dash", "ksh", "busybox"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	for d := range All.Members() {
		if got, ok := FromName(d.String()); !ok || got != d {
			t.Errorf("FromName(%q) = %v, %t; want %v", d.String(), got, ok, d)
		}
	}
	languages := map[Dialect]syntax.LangVariant{
		Bash: syntax.LangBash, Posix: syntax.LangBash, Dash: syntax.LangBash, Busybox: syntax.LangBash,
		Zsh: syntax.LangZsh, Ksh: syntax.LangKsh93,
	}
	for d, want := range languages {
		if got := d.Language(); got != want {
			t.Errorf("%s.Language() = %s, want %s", d, got, want)
		}
	}
	for name, use := range map[string]func(Dialect){
		"String":   func(d Dialect) { _ = d.String() },
		"Features": func(d Dialect) { _ = d.Features() },
		"Language": func(d Dialect) { _ = d.Language() },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s on an unknown dialect did not panic", name)
				}
			}()
			use(Busybox + 1)
		}()
	}
}

func TestEverySourceHasOneName(t *testing.T) {
	t.Parallel()

	seen := map[string]Source{}
	for s := SourceDefault; s <= SourceHostDefault; s++ {
		name := s.String()
		if other, dup := seen[name]; dup {
			t.Errorf("%d and %d are both named %q", other, s, name)
		}
		seen[name] = s
	}
	defer func() {
		if recover() == nil {
			t.Error("String on an unknown source did not panic")
		}
	}()
	_ = Source(sourceCount).String()
}

func TestInputBats(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   Input
		want bool
	}{
		{Input{Text: "#!/usr/bin/env bats\n@test x { :; }\n"}, true},
		{Input{Text: "#!/opt/bats/bin/bats\n"}, true},
		{Input{URI: "file:///t/unit.bats", Text: "@test x { :; }\n"}, true},
		{Input{URI: "file:///t/run.sh", Text: "#!/bin/bash\n# uses bats helpers\n"}, false},
		{Input{Text: "#!/bin/bash -e\n"}, false},
	} {
		if got := ShellOf(Bash, tc.in).Bats(); got != tc.want {
			t.Errorf("%+v Bats() = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestShellLanguage(t *testing.T) {
	t.Parallel()

	bats := Input{URI: "file:///t/unit.bats", Text: "#!/usr/bin/env bats\n"}
	for _, tc := range []struct {
		shell Shell
		want  syntax.LangVariant
	}{
		{ShellOf(Bash, bats), syntax.LangBats},
		{ShellOf(Zsh, bats), syntax.LangZsh},
		{ShellOf(Posix, bats), syntax.LangBash},
		{ShellOf(Ksh, Input{Text: "#!/bin/mksh\n"}), syntax.LangMirBSDKorn},
		{ShellOf(Bash, Input{Text: "#!/bin/mksh\n"}), syntax.LangBash},
		{FlavorBats.Shell(), syntax.LangBats},
		{FlavorMksh.Shell(), syntax.LangMirBSDKorn},
	} {
		if got := tc.shell.Lang(); got != tc.want {
			t.Errorf("%+v.Lang() = %s, want %s", tc.shell, got, tc.want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("a zsh shell with the bats flavor has a language")
		}
	}()
	_ = Shell{Dialect: Zsh, Flavor: FlavorBats}.Lang()
}

func TestInputMksh(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   Input
		want bool
	}{
		{Input{Text: "#!/bin/mksh\n"}, true},
		{Input{Text: "#!/usr/bin/env mksh\n"}, true},
		{Input{URI: "file:///home/u/.mkshrc"}, true},
		{Input{Text: "#!/bin/sh\n# shellcheck shell=mksh\n"}, true},
		{Input{Text: "#!/bin/mksh\n# shellcheck shell=ksh\n"}, false},
		{Input{Text: "#!/bin/ksh\n"}, false},
		{Input{Text: "#!/bin/ksh93\n"}, false},
		{Input{URI: "file:///w/old.ksh"}, false},
	} {
		if got := ShellOf(Ksh, tc.in).Flavor == FlavorMksh; got != tc.want {
			t.Errorf("%+v Mksh() = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRCFiles(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"/home/u/.zshrc": true, "/home/u/.bashrc": true, "/home/u/.profile": true, "/home/u/.mkshrc": true,
		"/home/u/zshrc.zsh": false, "/home/u/.bash_history": false, "": false,
	} {
		if got := IsRCFile(path); got != want {
			t.Errorf("IsRCFile(%q) = %v, want %v", path, got, want)
		}
	}
	for _, name := range RCFileNames() {
		if _, ok := FromFileName(name); !ok {
			t.Errorf("rc file %s has no dialect", name)
		}
	}
}

func TestNixShellShebangsNameTheirInterpreter(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]string{
		"#!/usr/bin/env nix-shell\n#! nix-shell -i bash -p jq\necho\n":             "bash",
		"#!/usr/bin/env nix-shell\n#!nix-shell -I nixpkgs=. -i zsh -p jq\n":        "zsh",
		"#!/usr/bin/env nix-shell\n#! nix-shell -p jq\n#! nix-shell -i dash\n":     "dash",
		"#!/usr/bin/env nix-shell\n#! nix-shell -p jq\necho\n#! nix-shell -i sh\n": "nix-shell",
	} {
		if got, ok := Interpreter(text); !ok || got != want {
			t.Errorf("Interpreter(%q) = %q, %v; want %q", text, got, ok, want)
		}
	}
	if d := Detect(nil, Input{URI: "file:///x/update.sh", Text: "#!/usr/bin/env nix-shell\n#! nix-shell -i bash -p jq\n"}); d != Bash {
		t.Errorf("a nix-shell bash script named .sh is detected as %v, want bash", d)
	}
}

func TestShared(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		dialects []Dialect
		want     Dialect
		fit      []Dialect
		misfit   []Dialect
	}{
		{nil, Posix, []Dialect{Bash}, []Dialect{Zsh}},
		{[]Dialect{Posix, Dash}, Dash, []Dialect{Bash}, []Dialect{Ksh}},
		{[]Dialect{Posix, Bash, Busybox}, Bash, []Dialect{Dash}, []Dialect{Zsh}},
		{[]Dialect{Zsh, Bash, Zsh}, Zsh, nil, []Dialect{Bash, Ksh}},
		{[]Dialect{Ksh, Bash}, Ksh, nil, []Dialect{Bash}},
	} {
		got, fits := Shared(tt.dialects)
		if got != tt.want {
			t.Errorf("Shared(%v) = %v, want %v", tt.dialects, got, tt.want)
		}
		for _, d := range tt.fit {
			if !fits(d) {
				t.Errorf("Shared(%v) leaves out %v", tt.dialects, d)
			}
		}
		for _, d := range tt.misfit {
			if fits(d) {
				t.Errorf("Shared(%v) keeps %v", tt.dialects, d)
			}
		}
	}
}
