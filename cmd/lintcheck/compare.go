package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/stubbedev/shell-lsp/internal/corpus"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func runCompare(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	examples := fs.Int("examples", 5, "mismatches to print per code and side")
	only := fs.String("code", "", "only report this code, e.g. SC2086")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errUsage
	}
	listing, err := corpus.ScriptsUnder(fs.Args())
	if err != nil {
		return err
	}
	c, err := compareWithShellcheck(ctx, listing)
	if err != nil {
		return err
	}
	report(os.Stdout, c, *examples, *only)
	return nil
}

func report(w io.Writer, c comparison, examples int, only string) {
	implemented := implementedCodes()
	selected := func(code lint.Code) bool { return only == "" || code.String() == only }
	fmt.Fprintf(w, "%d files analyzed, %d skipped, %d crashed shellcheck\n\n", c.compared, c.skipped, len(c.crashed))
	fmt.Fprintln(w, "| Code | Agree | Ours only | Shellcheck only | Precision | Recall |")
	fmt.Fprintln(w, "| --- | --- | --- | --- | --- | --- |")
	for _, code := range implemented {
		if !selected(code) {
			continue
		}
		t := c.tallies[code]
		fmt.Fprintf(w, "| %s | %d | %d | %d | %s | %s |\n", code, t.agree, t.oursOnly, t.theirsOnly, ratio(t.agree, t.agree+t.oursOnly), ratio(t.agree, t.agree+t.theirsOnly))
	}
	var current lint.Code
	shown := map[side]int{}
	for _, m := range c.mismatches {
		if !selected(m.code) {
			continue
		}
		if m.code != current {
			current, shown = m.code, map[side]int{}
		}
		if shown[m.side] >= examples {
			continue
		}
		if shown[oursOnly]+shown[theirsOnly] == 0 {
			fmt.Fprintf(w, "\n%s\n", m.code)
		}
		shown[m.side]++
		fmt.Fprintln(w, m)
	}
}

func (m mismatch) String() string {
	return fmt.Sprintf("  %s %s:%d:%d  %s", m.side.sign(), m.path, m.line, m.col, m.source)
}

func ratio(n, d int) string {
	if d == 0 {
		return "-"
	}
	return strconv.FormatFloat(100*float64(n)/float64(d), 'f', 1, 64) + "%"
}
