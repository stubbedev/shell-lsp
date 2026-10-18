package assist

import (
	"slices"
	"testing"
)

func TestWebURLs(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		text string
		want []string
	}{
		{"# docs: https://example.com/guide.", []string{"https://example.com/guide"}},
		{"# http://a.example/x?q=1&r=%20#top, then https://b.example", []string{"http://a.example/x?q=1&r=%20#top", "https://b.example"}},
		{"# see <https://example.com/a>", []string{"https://example.com/a"}},
		{`# curl "https://example.com/a" 'https://example.com/b'`, []string{"https://example.com/a", "https://example.com/b"}},
		{"# (see https://en.wikipedia.org/wiki/Bash_(Unix_shell))", []string{"https://en.wikipedia.org/wiki/Bash_(Unix_shell)"}},
		{"# [docs](https://example.com/docs)!", []string{"https://example.com/docs"}},
		{"# HTTPS://EXAMPLE.COM/A", []string{"HTTPS://EXAMPLE.COM/A"}},
		{"# https://user:pw@[2001:db8::1]:8080/p", []string{"https://user:pw@[2001:db8::1]:8080/p"}},
		{"# https://example.com/a]b", []string{"https://example.com/a"}},
		{"# https://de.wikipedia.org/wiki/Straße?", []string{"https://de.wikipedia.org/wiki/Straße"}},
		{"# https://example.com/{id}", []string{"https://example.com/"}},
		{"# https://example.com/a%zz", []string{"https://example.com/a"}},
		{"# https://example.com/a#b#c", []string{"https://example.com/a#b"}},
		{"# https://example.com/a\\b", []string{"https://example.com/a"}},
		{"# https://example.com/a`b`", []string{"https://example.com/a"}},
		{"# https://", nil},
		{"# https://.", nil},
		{"# https://).", nil},
		{"# https:// example.com", nil},
		{"# git+https://example.com/repo", nil},
		{"# xhttp://example.com", nil},
		{"# ftp://example.com", nil},
		{"# https:/example.com", nil},
		{"# https://[::1", nil},
		{"# https://example.com/a\xffb", []string{"https://example.com/a"}},
		{"# https://example.com/\uFFFE", []string{"https://example.com/"}},
	} {
		var got []string
		for span := range webURLs(tt.text) {
			got = append(got, tt.text[span.Start:span.End])
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("webURLs(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}
