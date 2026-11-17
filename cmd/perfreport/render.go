package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	sectionBegin = "<!-- perfreport:begin -->\n"
	sectionEnd   = "<!-- perfreport:end -->\n"
	bytesPerMiB  = 1 << 20
	bytesPerGiB  = 1 << 30
	microsPerMs  = 1000
	microsPerSec = 1000 * microsPerMs
	nanosPerUs   = 1000
)

var readmeKeystrokes = []struct{ name, label string }{
	{"nix-shell", "A keystroke with 700 scripts open, editing outside them"},
	{"nix-shell-typing", "A keystroke with 700 scripts open, typing in one"},
	{"nix", "A keystroke in a plain 5,000-line Nix file"},
}

func significant(v float64) string {
	decimals := 2
	switch {
	case v >= 100:
		decimals = 0
	case v >= 10:
		decimals = 1
	}
	return strconv.FormatFloat(v, 'f', decimals, 64)
}

func formatMicros(micros float64) string {
	switch {
	case micros >= microsPerSec:
		return significant(micros/microsPerSec) + " s"
	case micros >= microsPerMs:
		return significant(micros/microsPerMs) + " ms"
	}
	return significant(micros) + " µs"
}

func formatMiB(b int64) string {
	return strconv.FormatFloat(float64(b)/bytesPerMiB, 'f', 0, 64)
}

func formatCount(n int) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}

func formatUsage(u rss) string {
	text := formatMiB(u.PeakBytes) + " / " + formatMiB(u.IdleBytes) + " MiB"
	switch u.Restarts {
	case 0:
		return text
	case 1:
		return text + ", restarted once"
	}
	return fmt.Sprintf("%s, restarted %d times", text, u.Restarts)
}

func toolLabel(t tool) string { return t.Name + " " + t.Version }

func row(cells ...string) string { return "| " + strings.Join(cells, " | ") + " |\n" }

func renderSummary(r report) string {
	m := r.Machine
	return fmt.Sprintf("Measured by `cmd/perfreport` on shell-lsp %s, %s: %s, %d cores (%s), %s GiB of memory, %s/%s, %s. Times are medians of %d runs.\n",
		r.Version, r.Date, m.Runner, m.Cores, m.CPU, strconv.FormatFloat(float64(m.MemoryBytes)/bytesPerGiB, 'f', 0, 64), m.OS, m.Arch, r.Go, r.Repeats)
}

func renderTable(r report) (string, error) {
	var b strings.Builder
	b.WriteString(row("Measure", "shell-lsp", "For comparison"))
	b.WriteString(row("---", "---:", "---"))
	c := r.Check
	b.WriteString(row(
		fmt.Sprintf("`check` over the %s `*.sh` and `*.bash` files of %s", formatCount(c.Files), c.Project.Name),
		fmt.Sprintf("%s, %s of CPU", formatMicros(float64(c.Ours.WallMicros)), formatMicros(float64(c.Ours.CPUMicros))),
		fmt.Sprintf("one %s process: %s, %s times as long, with a different rule set", toolLabel(c.Reference), formatMicros(float64(c.Theirs.WallMicros)), significant(float64(c.Theirs.WallMicros)/float64(c.Ours.WallMicros))),
	))
	for _, k := range readmeKeystrokes {
		i := slices.IndexFunc(r.Keystrokes, func(got keystroke) bool { return got.Name == k.name })
		if i < 0 {
			return "", fmt.Errorf("the report has no %s/%s", keystrokeBenchmark, k.name)
		}
		b.WriteString(row(k.label, formatMicros(float64(r.Keystrokes[i].MedianNanos)/nanosPerUs), ""))
	}
	for _, m := range r.Memory {
		b.WriteString(row(
			fmt.Sprintf("Peak / idle memory on %s, %s files open", m.Project.Name, formatCount(m.Files)),
			formatUsage(m.Ours),
			toolLabel(m.Theirs.Server)+": "+formatUsage(m.Theirs),
		))
	}
	f := r.Format
	formatted := strconv.Itoa(f.Differences)
	if f.BothRejected > 0 {
		formatted += fmt.Sprintf(", and %s files both reject", formatCount(f.BothRejected))
	}
	b.WriteString(row(fmt.Sprintf("Formatting differences from %s over the %s `.nix` files of %s", toolLabel(f.Reference), formatCount(f.Files), f.Project.Name), formatted, ""))
	return b.String(), nil
}

func renderReadme(r report) (string, error) {
	table, err := renderTable(r)
	if err != nil {
		return "", err
	}
	return renderSummary(r) + "\n" + table, nil
}

func renderNotes(r report) (string, error) {
	body, err := renderReadme(r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("## Measured performance\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	b.WriteString(row("`"+keystrokeBenchmark+"`", "Median"))
	b.WriteString(row("---", "---:"))
	for _, k := range r.Keystrokes {
		b.WriteString(row(k.Name, formatMicros(float64(k.MedianNanos)/nanosPerUs)))
	}
	b.WriteString("\nPinned inputs: ")
	inputs := []string{inputLabel(r.Check.Project)}
	for _, m := range r.Memory {
		if label := inputLabel(m.Project); !slices.Contains(inputs, label) {
			inputs = append(inputs, label)
		}
	}
	b.WriteString(strings.Join(inputs, ", "))
	b.WriteString(".\n")
	return b.String(), nil
}

func inputLabel(p project) string { return p.Name + " `" + p.Commit + "`" }

func splitSection(doc string) (before, body, after string, err error) {
	before, rest, found := strings.Cut(doc, sectionBegin)
	if !found {
		return "", "", "", errors.New("no " + strings.TrimSpace(sectionBegin) + " line")
	}
	body, after, found = strings.Cut(rest, sectionEnd)
	if !found {
		return "", "", "", errors.New("no " + strings.TrimSpace(sectionEnd) + " line after " + strings.TrimSpace(sectionBegin))
	}
	return before, body, after, nil
}

func replaceSection(doc, section string) (string, error) {
	before, _, after, err := splitSection(doc)
	if err != nil {
		return "", err
	}
	return before + sectionBegin + section + sectionEnd + after, nil
}
