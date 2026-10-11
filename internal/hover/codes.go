package hover

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

func CodeDoc(info lint.CodeInfo) string {
	var sb strings.Builder
	sb.WriteString("**" + info.Code.String() + "** · " + info.Severity.String())
	if info.Optional != "" {
		sb.WriteString(" · optional, turned on with `enable=" + info.Optional + "`")
	}
	if info.Summary != "" {
		sb.WriteString("\n\n" + info.Summary)
	}
	sb.WriteString("\n\n[" + referenceTitle(info.Code) + "](" + info.Reference() + ")")
	return sb.String()
}

func referenceTitle(code lint.Code) string {
	if code.Native() {
		return code.String() + " in the shell-lsp rules"
	}
	return code.String() + " on the ShellCheck wiki"
}
