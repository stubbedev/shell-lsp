package nixeval

import (
	_ "embed"
	"net/url"
	"strings"
)

//go:embed eval.nix
var evalSource string

var nixStringEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)

func nixString(s string) string {
	return `"` + nixStringEscaper.Replace(s) + `"`
}

func nixStringList(items []string) string {
	var b strings.Builder
	b.WriteString("[")
	for _, item := range items {
		b.WriteString(" ")
		b.WriteString(nixString(item))
	}
	b.WriteString(" ]")
	return b.String()
}

type exprArg struct {
	name  string
	value string
}

func stringArg(name, value string) exprArg {
	return exprArg{name: name, value: nixString(value)}
}

func listArg(name string, items []string) exprArg {
	return exprArg{name: name, value: nixStringList(items)}
}

func callExpr(function string, args ...exprArg) string {
	var b strings.Builder
	b.WriteString("(")
	b.WriteString(evalSource)
	b.WriteString(").")
	b.WriteString(function)
	b.WriteString(" {")
	for _, arg := range args {
		b.WriteString(" ")
		b.WriteString(arg.name)
		b.WriteString(" = ")
		b.WriteString(arg.value)
		b.WriteString(";")
	}
	b.WriteString(" }")
	return b.String()
}

func lockedRef(storePath, narHash, dir string) string {
	query := url.Values{"narHash": {narHash}}
	if dir != "" {
		query.Set("dir", dir)
	}
	return "path:" + storePath + "?" + query.Encode()
}
