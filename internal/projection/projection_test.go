package projection

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func upper(_ string, _, _ int, text string) (string, bool) {
	return strings.ToUpper(text), true
}

func refuseQuotes(_ string, _, _ int, text string) (string, bool) {
	return text, !strings.Contains(text, "'")
}

func build(t *testing.T) (string, *Map) {
	t.Helper()
	host := "<<ab%cd>> [é f]"
	b := NewBuilder(host)
	b.Opaque(0, 2, "")
	b.Region(upper)
	b.Opaque(2, 2, "(")
	b.Verbatim(2, 4)
	b.Escape(4, 5, "x")
	b.Verbatim(5, 7)
	b.Opaque(7, 9, "")
	b.Region(refuseQuotes)
	b.Opaque(10, 11, "$$")
	b.Verbatim(11, len(host)-1)
	b.Opaque(len(host), len(host), ")")
	return host, b.Map()
}

func TestMapText(t *testing.T) {
	t.Parallel()

	_, m := build(t)
	if want := "(abxcd$$é f)"; m.Text() != want {
		t.Fatalf("Text() = %q, want %q", m.Text(), want)
	}
}

func TestHostOffset(t *testing.T) {
	t.Parallel()

	host, m := build(t)
	for _, tt := range []struct {
		proj ProjOffset
		host HostOffset
	}{
		{0, 2},
		{1, 2},
		{2, 3},
		{3, 4},
		{4, 5},
		{6, 10},
		{7, 10},
		{8, 11},
		{ProjOffset(len(m.Text()) - 1), HostOffset(len(host))},
		{ProjOffset(len(m.Text())), HostOffset(len(host))},
	} {
		if got := m.HostOffset(tt.proj); got != tt.host {
			t.Errorf("HostOffset(%d) = %d, want %d", tt.proj, got, tt.host)
		}
	}
}

func TestOffset(t *testing.T) {
	t.Parallel()

	host, m := build(t)
	for _, tt := range []struct {
		host   HostOffset
		proj   ProjOffset
		inside bool
	}{
		{0, 0, false},
		{2, 1, true},
		{4, 3, true},
		{5, 4, true},
		{7, 6, true},
		{8, 6, false},
		{1, 0, false},
		{9, 6, false},
		{10, 6, false},
		{11, 8, true},
		{HostOffset(len(host) - 1), ProjOffset(len(m.Text()) - 1), true},
		{HostOffset(len(host)), ProjOffset(len(m.Text())), false},
	} {
		got, inside := m.Offset(tt.host)
		if got != tt.proj || inside != tt.inside {
			t.Errorf("Offset(%d) = %d, %v, want %d, %v", tt.host, got, inside, tt.proj, tt.inside)
		}
	}
}

func TestEdit(t *testing.T) {
	t.Parallel()

	host, m := build(t)
	for _, tt := range []struct {
		name string
		edit textedit.Edit
		want string
		ok   bool
	}{
		{"replace inside a verbatim span", textedit.Edit{Start: 1, End: 2, NewText: "q"}, "<<Qb%cd>> [é f]", true},
		{"replace a whole escape", textedit.Edit{Start: 2, End: 4, NewText: "yz"}, "<<aYZcd>> [é f]", true},
		{"replace part of an escape", textedit.Edit{Start: 3, End: 3, NewText: "y"}, "<<abY%cd>> [é f]", true},
		{"insert at the end of a region", textedit.Edit{Start: 6, End: 6, NewText: "!"}, "<<ab%cd!>> [é f]", true},
		{"unchanged ends are trimmed", textedit.Edit{Start: 1, End: 6, NewText: "abQcd"}, "<<abQcd>> [é f]", true},
		{"trimming keeps whole runes", textedit.Edit{Start: 8, End: 10, NewText: "è"}, "<<ab%cd>> [è f]", true},
		{"the second region uses its own encoder", textedit.Edit{Start: 11, End: 12, NewText: "g"}, "<<ab%cd>> [é g]", true},
		{"its encoder can refuse", textedit.Edit{Start: 10, End: 11, NewText: "'"}, "", false},
		{"a synthetic start", textedit.Edit{Start: 0, End: 0, NewText: "x"}, "", false},
		{"across a placeholder", textedit.Edit{Start: 5, End: 9, NewText: "x"}, "", false},
		{"across two regions", textedit.Edit{Start: 4, End: 9, NewText: "x"}, "", false},
	} {
		got, ok := m.Edit(tt.edit)
		if ok != tt.ok {
			t.Errorf("%s: Edit(%+v) ok = %v, want %v", tt.name, tt.edit, ok, tt.ok)
			continue
		}
		if ok {
			if applied := textedit.Apply(host, []textedit.Edit{got}); applied != tt.want {
				t.Errorf("%s: applying %+v gives %q, want %q", tt.name, got, applied, tt.want)
			}
		}
	}
	if _, ok := m.Edits([]textedit.Edit{{Start: 1, End: 2, NewText: "q"}, {Start: 0, End: 0, NewText: "x"}}); ok {
		t.Error("Edits maps a group in which one edit has no place in the host")
	}
}

func TestBuilderRejectsDisorder(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		build func(b *Builder)
	}{
		{"backwards", func(b *Builder) {
			b.Region(upper)
			b.Verbatim(2, 3)
			b.Verbatim(0, 1)
		}},
		{"before a region", func(b *Builder) { b.Verbatim(0, 1) }},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: the builder accepted it", tt.name)
				}
			}()
			tt.build(NewBuilder("abc"))
		}()
	}
}
