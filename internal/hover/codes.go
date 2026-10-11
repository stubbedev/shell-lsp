package hover

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

func CodeDoc(info lint.CodeInfo) string {
	return codeHeader(info) + "\n\n" + references(info.Code)
}

func codeHeader(info lint.CodeInfo) string {
	var sb strings.Builder
	sb.WriteString("**" + info.Code.String() + "** · " + info.Severity.String())
	if info.Optional != "" {
		sb.WriteString(" · optional, turned on with `enable=" + info.Optional + "`")
	}
	if info.Summary != "" {
		sb.WriteString("\n\n" + info.Summary)
	}
	return sb.String()
}

func references(code lint.Code) string {
	links := "[" + code.String() + " in the shell-lsp " + docsPage(code) + "](" + code.Docs() + ")"
	if wiki, ok := code.Wiki(); ok {
		links += " · [ShellCheck wiki](" + wiki + ")"
	}
	return links
}

func docsPage(code lint.Code) string {
	if code.Native() {
		return "rules"
	}
	return "checks"
}

func referenceTitle(code lint.Code) string {
	if code.Native() {
		return code.String() + " in the shell-lsp rules"
	}
	return code.String() + " on the ShellCheck wiki"
}
