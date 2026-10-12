package lint

import "testing"

func TestSeverityNamesRoundTrip(t *testing.T) {
	t.Parallel()

	for _, s := range []Severity{SeverityError, SeverityWarning, SeverityInfo, SeverityStyle} {
		if got, ok := SeverityNamed(s.String()); !ok || got != s {
			t.Errorf("SeverityNamed(%q) = %v, %t, want %v", s.String(), got, ok, s)
		}
	}
	if _, ok := SeverityNamed(""); ok {
		t.Error("SeverityNamed accepted the empty name of the zero severity")
	}
}

func TestUnnamedValuesPanic(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]func() string{
		"zero severity":    Severity(0).String,
		"severity past":    (SeverityStyle + 1).String,
		"profile past end": (ProfileStrict + 1).String,
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("the %s was given a name", name)
				}
			}()
			value()
		}()
	}
}
