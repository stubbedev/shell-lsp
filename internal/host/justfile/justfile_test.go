package justfile

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestProjectText(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, justfile, want string
	}{
		{
			name:     "each line of a recipe runs in its own shell",
			justfile: "build:\n    cd src\n    cargo build\n",
			want:     "(\ncd src\n)\n(\ncargo build\n)\n",
		},
		{
			name:     "sigils and interpolations",
			justfile: "test name='x':\n  @echo {{name}} {{ uppercase(name) }}\n  -@rm '{{name}}.tmp'\n  @-echo {{{{literal}}\n",
			want:     "(\necho $$ $$\n)\n(\nrm ''$$'.tmp'\n)\n(\necho {{literal}}\n)\n",
		},
		{
			name:     "continuations join into one line",
			justfile: "x:\n    for f in a b; do \\\n      echo $f; \\\n    done\n",
			want:     "(\nfor f in a b; do echo $f; done\n)\n",
		},
		{
			name:     "shebang recipes are one script without the shebang",
			justfile: "x:\n    #!/usr/bin/env bash\n    set -euo pipefail\n    cat <<EOF\n    it's {{x}}\n    EOF\n    echo '{{x}}'\n",
			want:     "(\nset -euo pipefail\ncat <<EOF\nit's $$\nEOF\necho ''$$''\n)\n",
		},
		{
			name:     "shebang recipes keep sigils as shell",
			justfile: "x:\n    #!/usr/bin/env bash\n    @echo quiet\n    -echo ignored\n",
			want:     "(\n@echo quiet\n-echo ignored\n)\n",
		},
		{
			name:     "script attributes keep sigils as shell",
			justfile: "[script]\nx:\n    @echo quiet\n",
			want:     "(\n@echo quiet\n)\n",
		},
		{
			name:     "a shebang recipe of only comments opens no empty subshell",
			justfile: "x:\n    #!/usr/bin/env bash\n    # nothing to run yet\n",
			want:     "# nothing to run yet\n",
		},
		{
			name:     "script attributes",
			justfile: "[script]\nx:\n    a=1\n    echo $a\n\n[script(\"python3\")]\ny:\n    print(1)\n",
			want:     "(\na=1\necho $a\n)\n",
		},
		{
			name:     "comment lines are directives",
			justfile: "x:\n    # shellcheck disable=SC2086\n    echo $1\n",
			want:     "# shellcheck disable=SC2086\n(\necho $1\n)\n",
		},
		{
			name:     "just syntax around recipes is not shell",
			justfile: "set shell := [\"bash\", \"-uc\"]\nalias b := build\nversion := `git describe`\n# a comment\n[private]\nbuild target=\"a:b\": (dep \"x\") other\n    echo {{target}}\n\nother:\n\techo other\n",
			want:     "(\necho $$\n)\n(\necho other\n)\n",
		},
		{
			name:     "exported variables and parameters are set",
			justfile: "export token := 'x'\nrun $user other:\n    echo $token $user $other\n",
			want:     "(\ntoken=$$\nuser=$$\necho $token $user $other\n)\n",
		},
		{
			name:     "set export exports everything",
			justfile: "set export\nlevel := '1'\nrun arg:\n    echo $level $arg\n",
			want:     "(\narg=$$\nlevel=$$\necho $level $arg\n)\n",
		},
		{
			name:     "windows recipes run with the windows shell",
			justfile: "set windows-shell := [\"powershell.exe\", \"-c\"]\n[windows]\nx:\n    Get-ChildItem\n[unix]\nx:\n    ls\n",
			want:     "(\nls\n)\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := Project(tt.justfile)
			if m.Text() != tt.want {
				t.Errorf("Project(%q) =\n%q\nwant\n%q", tt.justfile, m.Text(), tt.want)
			}
		})
	}
}

func TestProjectDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, justfile string
		want           dialect.Detection
		recipes        int
	}{
		{"default shell", "x:\n    echo\n", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: "just"}, 1},
		{"set shell", "set shell := [\"bash\", \"-uc\"]\nx:\n    echo\n", dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostSetting, Evidence: `set shell := ["bash", "-uc"]`}, 1},
		{"widest of the sh family", "x:\n    echo\ny:\n    #!/usr/bin/env bash\n    echo\n", dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostSetting, Evidence: "#!/usr/bin/env bash"}, 2},
		{"the family most recipes use", "a:\n    #!/bin/zsh\n    print\nb:\n    #!/bin/zsh\n    print\nc:\n    echo\n", dialect.Detection{Dialect: dialect.Zsh, Source: dialect.SourceHostSetting, Evidence: "#!/bin/zsh"}, 2},
		{"script interpreter", "set script-interpreter := ['dash', '-eu']\n[script]\nx:\n    echo\n", dialect.Detection{Dialect: dialect.Dash, Source: dialect.SourceHostSetting, Evidence: "set script-interpreter := ['dash', '-eu']"}, 1},
		{"script attribute", "[script('bash')]\nx:\n    echo\n", dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostSetting, Evidence: "[script('bash')]"}, 1},
		{"no shell at all", "set shell := [\"nu\", \"-c\"]\nx:\n    ls\n", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: "just"}, 0},
	} {
		m, got := Project(tt.justfile)
		if got != tt.want {
			t.Errorf("%s: detection = %+v, want %+v", tt.name, got, tt.want)
		}
		if n := strings.Count(m.Text(), "\n)\n"); n != tt.recipes {
			t.Errorf("%s: %d recipes analysed, want %d:\n%s", tt.name, n, tt.recipes, m.Text())
		}
	}
}

func TestProjectPositions(t *testing.T) {
	t.Parallel()

	src := "x:\n    @echo {{name}} done\n"
	m, _ := Project(src)
	for _, tt := range []struct{ proj, host string }{{"echo", "echo"}, {"$$", "{{name}}"}, {"done", "done"}} {
		start, hostStart := projection.ProjOffset(strings.Index(m.Text(), tt.proj)), projection.HostOffset(strings.Index(src, tt.host))
		if got := m.HostOffset(start); got != hostStart {
			t.Errorf("HostOffset of %q = %d, want %d", tt.proj, got, hostStart)
		}
		if got := m.HostOffset(start + projection.ProjOffset(len(tt.proj))); got != hostStart+projection.HostOffset(len(tt.host)) {
			t.Errorf("HostOffset of the end of %q = %d, want %d", tt.proj, got, hostStart+projection.HostOffset(len(tt.host)))
		}
	}
	if _, ok := m.Offset(projection.HostOffset(strings.Index(src, "@"))); ok {
		t.Error("the @ sigil is shell")
	}
}

func TestProjectEdits(t *testing.T) {
	t.Parallel()

	linewise := "x:\n    echo `date` {{a}}\n"
	script := "x:\n    #!/bin/sh\n    echo `date`\n"
	for _, tt := range []struct {
		name, src, old, text, want string
		ok                         bool
	}{
		{"backticks", linewise, "`date`", "$(date)", "x:\n    echo $(date) {{a}}\n", true},
		{"braces are escaped", linewise, "`date`", "${x}{{y}}", "x:\n    echo ${x}{{{{y}} {{a}}\n", true},
		{"no new lines in a linewise recipe", linewise, "`date`", "a\nb", "", false},
		{"new lines keep the indentation of a script", script, "`date`", "a\nb", "x:\n    #!/bin/sh\n    echo a\n    b\n", true},
		{"a brace next to a brace", "x:\n    echo {a}\n", "a", "{", "", false},
	} {
		m, _ := Project(tt.src)
		start := strings.Index(m.Text(), tt.old)
		got, ok := m.Edit(textedit.Edit{Start: start, End: start + len(tt.old), NewText: tt.text})
		if ok != tt.ok {
			t.Errorf("%s: ok = %v, want %v", tt.name, ok, tt.ok)
			continue
		}
		if applied := textedit.Apply(tt.src, []textedit.Edit{got}); ok && applied != tt.want {
			t.Errorf("%s: applying %+v gives %q, want %q", tt.name, got, applied, tt.want)
		}
	}
}
