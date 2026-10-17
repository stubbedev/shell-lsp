package oracle

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Verdict struct {
	Accepted     bool
	Inconclusive bool
	Line         int
	Column       int
	Message      string
}

const inconclusive = "inconclusive: "

func (v Verdict) String() string {
	switch {
	case v.Inconclusive:
		return inconclusive + v.Message
	case v.Accepted:
		return "accepted"
	}
	return fmt.Sprintf("rejected %d:%d %s", v.Line, v.Column, v.Message)
}

func Parse(s string) (Verdict, bool) {
	if s == "accepted" {
		return Verdict{Accepted: true}, true
	}
	if message, ok := strings.CutPrefix(s, inconclusive); ok {
		return Verdict{Inconclusive: true, Message: message}, true
	}
	rest, ok := strings.CutPrefix(s, "rejected ")
	if !ok {
		return Verdict{}, false
	}
	position, message, _ := strings.Cut(rest, " ")
	lineText, columnText, _ := strings.Cut(position, ":")
	line, err1 := strconv.Atoi(lineText)
	column, err2 := strconv.Atoi(columnText)
	if err1 != nil || err2 != nil {
		return Verdict{}, false
	}
	return Verdict{Line: line, Column: column, Message: message}, true
}

var justLocation = regexp.MustCompile(`(?m)^\s*——▶ [^:]+:(\d+):(\d+)`)

func Just(ctx context.Context, justfile, dir string, env []string) Verdict {
	cmd := exec.CommandContext(ctx, "just", "--justfile", justfile, "--working-directory", dir, "--dump") //nolint:gosec // G204: just only reads the justfile it is given
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return Verdict{Accepted: true}
	}
	out := stderr.String()
	message, _, _ := strings.Cut(strings.TrimPrefix(out, "error: "), "\n")
	v := Verdict{Message: message}
	if m := justLocation.FindStringSubmatch(out); m != nil {
		v.Line, _ = strconv.Atoi(m[1])
		v.Column, _ = strconv.Atoi(m[2])
	}
	return v
}

func Disagreement(v Verdict, lines int, errorLines []int) (string, bool) {
	if v.Inconclusive {
		return "", false
	}
	if v.Line > lines {
		v.Line = lines
	}
	switch {
	case v.Accepted && len(errorLines) > 0:
		return fmt.Sprintf("the reference tool accepts it, but errors are reported on lines %v", errorLines), true
	case !v.Accepted && v.Line == 0 && len(errorLines) == 0:
		return fmt.Sprintf("the reference tool rejects it (%s), but no error is reported", v.Message), true
	case !v.Accepted && v.Line > 0 && !slices.Contains(errorLines, v.Line):
		return fmt.Sprintf("the reference tool rejects line %d (%s), but errors are reported on lines %v", v.Line, v.Message, errorLines), true
	}
	return "", false
}
