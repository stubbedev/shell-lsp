package manpagetest

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/manpage"
)

type Entry struct {
	Tag  string
	Body string
}

var (
	Markup = Entry{
		Tag:  `\fB\-c\fR \fI<new\-branch>\fR, \fB\-\-create\fR \fI<new\-branch>\fR`,
		Body: `Create a branch named \fI<new\-branch>\fR, matching *glob* or snake_case.`,
	}
	MarkupNames    = "-c, --create"
	MarkupMarkdown = `Create a branch named *\<new-branch\>*, matching \*glob\* or snake\_case.`
)

func Options(entries ...Entry) []manpage.Option {
	var src strings.Builder
	src.WriteString(".TH TOOL 1\n.SH NAME\ntool \\- a tool\n.SH OPTIONS\n")
	for _, e := range entries {
		src.WriteString(".TP\n" + e.Tag + "\n" + e.Body + "\n")
	}
	page, err := manpage.Render([]byte(src.String()))
	if err != nil {
		panic(err)
	}
	return page.Options()
}
