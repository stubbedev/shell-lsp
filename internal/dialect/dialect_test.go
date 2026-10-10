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
		{name: "shellcheck directive mid-file", in: Input{
			URI: "file:///w/untitled", LanguageID: "plaintext",
			Text: "#!/bin/sh\n# shellcheck shell=ksh\n",
		}, want: Posix},
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

func TestShellcheckName(t *testing.T) {
	t.Parallel()

	want := map[Dialect]string{Bash: "bash", Posix: "sh", Dash: "dash", Ksh: "ksh"}
	for d, name := range want {
		if got, ok := d.ShellcheckName(); !ok || got != name {
			t.Errorf("%s.ShellcheckName() = %q, %t; want %q", d, got, ok, name)
		}
	}
	if got, ok := Zsh.ShellcheckName(); ok {
		t.Errorf("Zsh.ShellcheckName() = %q, want unsupported", got)
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
	if !Dash.Features().Local {
		t.Error("dash local support missing")
	}
	if Posix.Features().Local {
		t.Error("posix sh claims local support")
	}
}
