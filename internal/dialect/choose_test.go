package dialect

import (
	"slices"
	"testing"
)

func TestChoose(t *testing.T) {
	t.Parallel()

	setting := func(d Dialect, evidence string) Detection {
		return Detection{Dialect: d, Source: SourceHostSetting, Evidence: evidence}
	}
	byDefault := Detection{Dialect: Posix, Source: SourceHostDefault, Evidence: "tool"}
	for _, tt := range []struct {
		name    string
		scripts []Detection
		want    Detection
		kept    []Detection
	}{
		{"nothing to choose from", nil, byDefault, nil},
		{
			"the widest shell of the larger family",
			[]Detection{setting(Posix, "sh"), setting(Zsh, "zsh"), setting(Bash, "bash")},
			setting(Bash, "bash"),
			[]Detection{setting(Posix, "sh"), setting(Bash, "bash")},
		},
		{
			"the first script that names the shared shell",
			[]Detection{setting(Ksh, "a"), setting(Ksh, "b")},
			setting(Ksh, "a"),
			[]Detection{setting(Ksh, "a"), setting(Ksh, "b")},
		},
	} {
		got, kept := Choose(tt.scripts, func(d Detection) Detection { return d }, "tool")
		if got != tt.want || !slices.Equal(kept, tt.kept) {
			t.Errorf("%s: Choose = %+v, %+v; want %+v, %+v", tt.name, got, kept, tt.want, tt.kept)
		}
	}
}
