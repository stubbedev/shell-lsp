package fileuri

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

func ToPath(uri string) (string, error) { return toPath(platform.Native, uri) }

func FromPath(path string) string { return fromPath(platform.Native, path) }

func Key(uri string) string { return key(platform.Native, uri) }

func toPath(f platform.Flavour, uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parsing document URI %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("document URI %q is not a file URI", uri)
	}
	remote := u.Host != "" && u.Host != "localhost"
	switch {
	case remote && f == platform.Windows:
		return f.Clean("//" + u.Host + u.Path), nil
	case remote:
		return "", fmt.Errorf("document URI %q names remote host %q", uri, u.Host)
	case f == platform.Windows && strings.HasPrefix(u.Path, "/") && platform.HasDrive(u.Path[1:]):
		return f.Clean(u.Path[1:]), nil
	}
	return f.Clean(u.Path), nil
}

func fromPath(f platform.Flavour, path string) string {
	if f == platform.POSIX {
		return (&url.URL{Scheme: "file", Path: path}).String()
	}
	slashed := strings.ReplaceAll(path, `\`, "/")
	if rest, ok := strings.CutPrefix(slashed, "//"); ok {
		host, tail, _ := strings.Cut(rest, "/")
		return (&url.URL{Scheme: "file", Host: host, Path: "/" + tail}).String()
	}
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func key(f platform.Flavour, uri string) string {
	path, err := toPath(f, uri)
	if err != nil {
		return uri
	}
	return fromPath(f, f.Fold(path))
}
