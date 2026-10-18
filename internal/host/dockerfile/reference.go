package dockerfile

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

const maxTag = 128

type Reference struct {
	Domain string
	Path   string
	Tag    string
	Digest string
}

func ParseReference(ref string) (Reference, bool) {
	var r Reference
	name, digest, hasDigest := strings.Cut(ref, "@")
	if hasDigest {
		if !validDigest(digest) {
			return Reference{}, false
		}
		r.Digest = digest
	}
	if slash := strings.LastIndexByte(name, '/'); strings.LastIndexByte(name, ':') > slash {
		colon := strings.LastIndexByte(name, ':')
		if !validTag(name[colon+1:]) {
			return Reference{}, false
		}
		r.Tag, name = name[colon+1:], name[:colon]
	}
	parts := strings.Split(name, "/")
	if len(parts) > 1 && (strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost") {
		if !validDomain(parts[0]) {
			return Reference{}, false
		}
		r.Domain, parts = parts[0], parts[1:]
	}
	for _, p := range parts {
		if !validComponent(p) {
			return Reference{}, false
		}
	}
	r.Path = strings.Join(parts, "/")
	return r, true
}

func validReference(ref string) bool {
	_, ok := ParseReference(ref)
	return ok
}

func validComponent(p string) bool {
	if p == "" || !scan.LowerOrDigit(p[0]) || !scan.LowerOrDigit(p[len(p)-1]) {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if scan.LowerOrDigit(c) {
			continue
		}
		j := i
		for j < len(p) && !scan.LowerOrDigit(p[j]) {
			j++
		}
		sep := p[i:j]
		if sep != "." && sep != "_" && sep != "__" && strings.Trim(sep, "-") != "" {
			return false
		}
		i = j - 1
	}
	return true
}

func validTag(tag string) bool {
	if tag == "" || len(tag) > maxTag || !scan.NameChar(tag[0]) {
		return false
	}
	for i := range len(tag) {
		if c := tag[i]; !scan.NameChar(c) && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

func validDomain(domain string) bool {
	host, port, hasPort := strings.Cut(domain, ":")
	if hasPort && (port == "" || strings.Trim(port, "0123456789") != "") {
		return false
	}
	return host != "" && !strings.ContainsFunc(host, func(c rune) bool {
		return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '-'
	})
}

func validDigest(digest string) bool {
	algorithm, hex, ok := strings.Cut(digest, ":")
	return ok && algorithm != "" && len(hex) >= 32 && strings.Trim(hex, "0123456789abcdef") == ""
}
