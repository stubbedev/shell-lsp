package parser

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func indexedNames(tree *Tree) (defs, commands, keywords []string) {
	for _, d := range tree.Definitions {
		defs = append(defs, d.Name)
	}
	for _, c := range tree.Commands {
		commands = append(commands, c.Name)
	}
	for _, k := range tree.Keywords {
		keywords = append(keywords, k.Name)
	}
	return defs, commands, keywords
}

func TestZshAlternateLoopsAndConditionalsAreIndexed(t *testing.T) {
	t.Parallel()

	src := "for k v (a 1 b 2) print $k $v\n" +
		"repeat 3 print hi\n" +
		"foreach f (x y)\n  print $f\nend\n" +
		"while (( i < 3 )) { (( i++ )) }\n" +
		"if [[ -n $x ]] { print yes } elif [[ -n $y ]] print maybe\n" +
		"if [[ -z $x ]] { print no } else { print yes }\n" +
		"for (( j = 0; j < 2; j++ )) print $j\n"
	tree := Parse(src, dialect.Zsh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	defs, commands, keywords := indexedNames(tree)
	if want := []string{"k", "v", "f", "i", "j", "j"}; !slices.Equal(defs, want) {
		t.Errorf("definitions = %v, want %v", defs, want)
	}
	if want := []string{"print", "print", "print", "print", "print", "print", "print", "print"}; !slices.Equal(commands, want) {
		t.Errorf("commands = %v, want %v", commands, want)
	}
	if want := []string{"for", "repeat", "foreach", "end", "while", "if", "[[", "]]", "elif", "[[", "]]", "if", "[[", "]]", "else", "for"}; !slices.Equal(keywords, want) {
		t.Errorf("keywords = %v, want %v", keywords, want)
	}
	for _, k := range tree.Keywords {
		if src[k.Span.Start:k.Span.End] != k.Name {
			t.Errorf("keyword %s spans %q", k.Name, src[k.Span.Start:k.Span.End])
		}
	}
}

func TestZshLoopVariablesAreReferencedInShortBodies(t *testing.T) {
	t.Parallel()

	tree := Parse("for f (*.txt) print -r -- $f\n", dialect.Zsh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	if len(tree.References) != 1 || tree.References[0].Name != "f" {
		t.Fatalf("references = %v, want one to f", tree.References)
	}
	ref := tree.References[0]
	kinds := []DefinitionKind{}
	for def := range tree.Visible(ref.Name, ref.Within).All() {
		kinds = append(kinds, def.Kind)
	}
	if !slices.Equal(kinds, []DefinitionKind{DefLoop}) {
		t.Errorf("definitions visible from the reference = %v, want the loop variable", kinds)
	}
}

func TestZshAnonymousFunctionArgumentsBelongToTheCaller(t *testing.T) {
	t.Parallel()

	tree := Parse("() {\n  local x=$1\n  print $x\n} $y z\n", dialect.Zsh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	if len(tree.Scopes) != 2 {
		t.Fatalf("scopes = %v, want the file and the anonymous function body", tree.Scopes)
	}
	within := map[string]ScopeID{}
	for _, r := range tree.References {
		within[r.Name] = r.Within
	}
	if within["x"] != 1 || within["y"] != FileScope {
		t.Errorf("reference scopes = %v, want x in the body and y in the file", within)
	}
	if len(tree.Definitions) != 1 || !tree.Definitions[0].Local || tree.Definitions[0].Within != 1 {
		t.Errorf("definitions = %+v, want x local to the body", tree.Definitions)
	}
}

func TestZshFunctionsWithSeveralNames(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"start stop () { print $0 }\n",
		"function start stop { print $0 }\n",
		"function start stop; print $0\n",
	} {
		tree := Parse(src, dialect.Zsh)
		if len(tree.Errors) != 0 {
			t.Fatalf("%q: errors = %v, want none", src, messages(tree))
		}
		var names []string
		for _, f := range tree.Functions {
			names = append(names, f.Name)
		}
		if !slices.Equal(names, []string{"start", "stop"}) {
			t.Errorf("%q: functions = %v, want start and stop", src, names)
		}
	}
}

func TestZshTryAlwaysIsIndexed(t *testing.T) {
	t.Parallel()

	src := "{\n  x=1\n  print try\n} always {\n  print $x\n}\n"
	tree := Parse(src, dialect.Zsh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	defs, commands, keywords := indexedNames(tree)
	if !slices.Equal(defs, []string{"x"}) || !slices.Equal(commands, []string{"print", "print"}) || !slices.Equal(keywords, []string{"always"}) {
		t.Errorf("definitions %v, commands %v, keywords %v; want x, two prints and always", defs, commands, keywords)
	}
}

func TestZshCoprocCaseGroupsAndClosingBracesAreIndexed(t *testing.T) {
	t.Parallel()

	src := "coproc cat -u\n" +
		"case $x in (a|b)*) print $x ;; ((#i)c) print c ;; esac\n" +
		"f() { local y=1 }\n" +
		"case $x { (d) print d ;; }\n" +
		"[[ -N $file ]]\n"
	tree := Parse(src, dialect.Zsh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	defs, commands, keywords := indexedNames(tree)
	if !slices.Equal(defs, []string{"y"}) {
		t.Errorf("definitions = %v, want y", defs)
	}
	if want := []string{"cat", "print", "print", "local", "print"}; !slices.Equal(commands, want) {
		t.Errorf("commands = %v, want %v", commands, want)
	}
	if want := []string{"coproc", "case", "in", "esac", "case", "[[", "]]"}; !slices.Equal(keywords, want) {
		t.Errorf("keywords = %v, want %v", keywords, want)
	}
}

func TestZshArithmeticFormsAreIndexed(t *testing.T) {
	t.Parallel()

	tree := Parse("(( y = sqrt(x) + #c + ##d ))\nprint $(( [#16] n )) ${s:/a/b} ${s::=v} ${s:^t}\n", dialect.Zsh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	defs, _, _ := indexedNames(tree)
	if !slices.Equal(defs, []string{"y"}) {
		t.Errorf("definitions = %v, want y", defs)
	}
	var refs []string
	for _, r := range tree.References {
		refs = append(refs, r.Name)
	}
	if want := []string{"x", "c", "n", "s", "s", "s", "t"}; !slices.Equal(refs, want) {
		t.Errorf("references = %v, want %v", refs, want)
	}
}
