package dockerfile

import (
	"slices"
	"testing"
)

func TestCopySourcesBecomeUnquotedPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{name: "bare source", source: "notes.txt", want: []string{"notes.txt"}},
		{name: "double quoted source", source: `"notes.txt"`, want: []string{"notes.txt"}},
		{name: "single quoted source", source: `'notes.txt'`, want: []string{"notes.txt"}},
		{name: "partly quoted source", source: `"notes".txt`, want: []string{"notes.txt"}},
		{name: "json source", source: `["notes.txt",`, want: []string{"notes.txt"}},
		{name: "quoted variable", source: `"$X/a"`, want: []string{}},
		{name: "single quoted variable", source: `'$X/a'`, want: []string{}},
		{name: "escaped variable", source: `\$X/a`, want: []string{}},
		{name: "quoted url", source: `"https://example.com/a"`, want: []string{}},
		{name: "quoted glob", source: `"*.txt"`, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text := "FROM alpine:3\nCOPY " + tt.source + " /srv/\n"
			if tt.source[0] == '[' {
				text = "FROM alpine:3\nCOPY " + tt.source + ` "/srv/"]` + "\n"
			}
			f := Parse(text)
			got := []string{}
			for _, p := range f.Index(nil).Paths {
				got = append(got, p.Path)
				if p.Span.Start >= p.Span.End || p.Span.End > len(text) {
					t.Errorf("path %q has span %v outside %q", p.Path, p.Span, text)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("paths of %q = %q, want %q", text, got, tt.want)
			}
		})
	}
}
