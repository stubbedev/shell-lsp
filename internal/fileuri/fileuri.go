package fileuri

import (
	"fmt"
	"net/url"
	"path/filepath"
)

func ToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parsing document URI %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("document URI %q is not a file URI", uri)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("document URI %q names remote host %q", uri, u.Host)
	}
	return filepath.FromSlash(u.Path), nil
}

func FromPath(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
