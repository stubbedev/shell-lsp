package parser

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestZshNameOperands(t *testing.T) {
	t.Parallel()

	src := "_describe -t cmds 'command' cmds extra -- more\n" +
		"compadd -J group -O out -a first second[1,2]\n" +
		"_wanted hosts expl 'host' compadd -a hosts\n" +
		"_store_cache id one two\n" +
		"zstyle -s ':ctx' style value\n" +
		"zstyle -g names\n" +
		"print -v greeting -r -- hi\n" +
		"strftime -s today '%F'\n" +
		"zparseopts -D -a all -- h=help -out:=output\n" +
		"vared -p 'name? ' name\n" +
		"zformat -f line '%n' n:x\n" +
		"zformat -a pairs : a:1\n" +
		"zstat -H info -A times file\n" +
		"zselect -t 10 -a ready 3\n" +
		"sysread -c count -i 3 chunk\n"
	for d, want := range map[dialect.Dialect]struct{ defs, refs []string }{
		dialect.Zsh: {
			defs: []string{
				"out", "expl", "value", "names", "greeting", "today", "all", "help", "output", "name",
				"line", "pairs", "info", "times", "ready", "count", "chunk",
			},
			refs: []string{"cmds", "extra", "more", "first", "second", "expl", "hosts", "one", "two", "name"},
		},
		dialect.Bash: {},
	} {
		tree := Parse(src, d)
		if len(tree.Errors) != 0 {
			t.Fatalf("%s: errors = %v, want none", d, messages(tree))
		}
		var defs, refs []string
		for _, def := range tree.Definitions {
			defs = append(defs, def.Name)
			if src[def.NameSpan.Start:def.NameSpan.End] != def.Name {
				t.Errorf("%s: definition %s spans %q", d, def.Name, src[def.NameSpan.Start:def.NameSpan.End])
			}
		}
		for _, r := range tree.References {
			refs = append(refs, r.Name)
			if src[r.Span.Start:r.Span.End] != r.Name {
				t.Errorf("%s: reference %s spans %q", d, r.Name, src[r.Span.Start:r.Span.End])
			}
		}
		if !slices.Equal(defs, want.defs) {
			t.Errorf("%s: definitions = %v, want %v", d, defs, want.defs)
		}
		if !slices.Equal(refs, want.refs) {
			t.Errorf("%s: references = %v, want %v", d, refs, want.refs)
		}
	}
}
