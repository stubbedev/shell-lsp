package dialect

import "testing"

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
		if !f.GlobsUnquoted || !f.SplitsUnquoted || f.ArithmeticStatus || f.AssignsPositionals || f.AssociativePairs {
			t.Errorf("%s claims zsh's expansion, status or assignment rules", d)
		}
	}
	if z := Zsh.Features(); z.GlobsUnquoted || z.SplitsUnquoted || !z.ArithmeticStatus || !z.AssignsPositionals || !z.AssociativePairs {
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
	for _, d := range []Dialect{Bash, Zsh, Posix, Dash, Ksh, Busybox} {
		if !All.Has(d) {
			t.Errorf("All lacks %s", d)
		}
	}
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
		if got := tc.in.Bats(); got != tc.want {
			t.Errorf("%+v Bats() = %v, want %v", tc.in, got, tc.want)
		}
	}
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
		if got := tc.in.Mksh(); got != tc.want {
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
