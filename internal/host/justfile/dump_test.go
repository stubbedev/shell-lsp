package justfile

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

var update = flag.Bool("update", false, "rewrite the golden files from the just and make reference binaries")

func justDump(t *testing.T, path string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "just", "--justfile", path, "--working-directory", filepath.Dir(path), "--dump", "--dump-format", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("just --dump %s: %v\n%s", path, err, stderr.String())
	}
	return out
}

func TestParseAgreesWithJust(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("testdata", "parse", "*.just"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no parse fixtures: %v", err)
	}
	if *update {
		hosttest.RequireTool(t, "just", hosttest.JustVersion)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			golden := strings.TrimSuffix(path, ".just") + ".json"
			if *update {
				var raw map[string]any
				if err := json.Unmarshal(justDump(t, path), &raw); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.MarshalIndent(normalizeDump(raw), "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, append(encoded, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wantRaw, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v; record it with go test -run TestParseAgreesWithJust -update", err)
			}
			var want any
			if err := json.Unmarshal(wantRaw, &want); err != nil {
				t.Fatal(err)
			}
			f := Parse(string(text))
			if len(f.Errors) > 0 {
				t.Fatalf("parse errors in a file just accepts: %+v", f.Errors)
			}
			encoded, err := json.Marshal(dump(f))
			if err != nil {
				t.Fatal(err)
			}
			var got any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("parse of %s differs from just (-just +ours):\n%s", path, diff)
			}
		})
	}
}

func field(m map[string]any, name string) map[string]any {
	out, _ := m[name].(map[string]any)
	return out
}

func normalizeDump(raw map[string]any) map[string]any {
	recipes := map[string]any{}
	for name, value := range field(raw, "recipes") {
		r := value.(map[string]any)
		params := []any{}
		for _, p := range r["parameters"].([]any) {
			param := p.(map[string]any)
			params = append(params, map[string]any{"name": param["name"], "kind": param["kind"], "export": param["export"], "default": param["default"]})
		}
		deps := []any{}
		for _, d := range r["dependencies"].([]any) {
			dep := d.(map[string]any)
			deps = append(deps, map[string]any{"recipe": dep["recipe"], "arguments": dep["arguments"]})
		}
		recipes[name] = map[string]any{
			"attributes":   attributeNames(r["attributes"].([]any)),
			"doc":          r["doc"],
			"quiet":        r["quiet"],
			"shebang":      r["shebang"],
			"private":      r["private"],
			"priors":       r["priors"],
			"parameters":   params,
			"dependencies": deps,
			"body":         r["body"],
		}
	}
	assignments := map[string]any{}
	for name, value := range field(raw, "assignments") {
		a := value.(map[string]any)
		assignments[name] = map[string]any{"export": a["export"], "eager": a["eager"], "private": a["private"], "value": a["value"]}
	}
	aliases := map[string]any{}
	for name, value := range field(raw, "aliases") {
		aliases[name] = value.(map[string]any)["target"]
	}
	modules := []any{}
	for name := range field(raw, "modules") {
		modules = append(modules, name)
	}
	slices.SortFunc(modules, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	settings := map[string]any{}
	for name, value := range field(raw, "settings") {
		switch v := value.(type) {
		case nil:
		case bool:
			if v {
				settings[name] = v
			}
		case []any:
			if len(v) > 0 {
				settings[name] = v
			}
		default:
			settings[name] = v
		}
	}
	return map[string]any{"recipes": recipes, "assignments": assignments, "aliases": aliases, "modules": modules, "settings": settings}
}

func attributeNames(attrs []any) []any {
	out := []any{}
	for _, a := range attrs {
		switch v := a.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			for name := range v {
				out = append(out, name)
			}
		}
	}
	slices.SortFunc(out, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	return out
}

var currentSystem = []string{"linux", "unix"}

var systemAttributes = []string{"android", "dragonfly", "freebsd", "linux", "macos", "netbsd", "openbsd", "unix", "windows"}

func enabled(attrs []Attribute) bool {
	conditional := false
	for _, a := range attrs {
		if slices.Contains(systemAttributes, a.Name.Text) {
			conditional = true
			if slices.Contains(currentSystem, a.Name.Text) {
				return true
			}
		}
	}
	return !conditional
}

func dump(f *File) map[string]any {
	d := dumper{text: f.Text}
	recipes := map[string]any{}
	assignments := map[string]any{}
	aliases := map[string]any{}
	modules := []any{}
	settings := map[string]any{}
	for _, it := range f.Items {
		switch it.Kind {
		case ItemRecipe:
			r := it.Recipe
			if !enabled(r.Attributes) {
				continue
			}
			recipes[r.Name.Text] = dumpRecipe(f, r)
		case ItemAssignment:
			a := it.Assignment
			if !enabled(a.Attributes) {
				continue
			}
			_, private := attributeNamed(a.Attributes, "private")
			assignments[a.Name.Text] = map[string]any{"export": a.Export, "eager": a.Eager, "private": private || strings.HasPrefix(a.Name.Text, "_"), "value": d.sexp(a.Value)}
		case ItemAlias:
			if enabled(it.Alias.Attributes) {
				aliases[it.Alias.Name.Text] = it.Alias.Target.String()
			}
		case ItemModule:
			if !it.Module.Optional {
				modules = append(modules, it.Module.Name.Text)
			}
		case ItemSetting:
			s := it.Setting
			if enabled(s.Attributes) {
				name := strings.ReplaceAll(s.Name.Text, "-", "_")
				if value, ok := dumpSetting(name, s.Value); ok {
					settings[name] = value
				}
			}
		case ItemUnexport, ItemImport, ItemFunction:
		}
	}
	return map[string]any{"recipes": recipes, "assignments": assignments, "aliases": aliases, "modules": modules, "settings": settings}
}

var listSettings = []string{"shell", "windows_shell", "script_interpreter"}

func dumpSetting(name string, value *Expr) (any, bool) {
	switch {
	case value == nil:
		return true, true
	case value.Kind == ExprVariable && value.Name.Text == "true":
		return true, true
	case value.Kind == ExprVariable && value.Name.Text == "false":
		return nil, false
	case slices.Contains(listSettings, name):
		words := Strings(value)
		args := []any{}
		for _, w := range words[1:] {
			args = append(args, w)
		}
		return map[string]any{"command": words[0], "arguments": args}, true
	case name == "dotenv_command":
		words := []any{}
		for _, w := range Strings(value) {
			words = append(words, w)
		}
		return words, true
	}
	return value.Str.Value, true
}

func dumpRecipe(f *File, r *Recipe) map[string]any {
	d := dumper{text: f.Text}
	names := []any{}
	for _, a := range r.Attributes {
		names = append(names, a.Name.Text)
	}
	slices.SortFunc(names, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	var doc any
	if r.Doc != "" {
		doc = r.Doc
	}
	if attr, ok := attributeNamed(r.Attributes, "doc"); ok {
		doc = nil
		if len(attr.Args) > 0 {
			doc = attr.Args[0].Value.Str.Value
		}
	}
	_, private := attributeNamed(r.Attributes, "private")
	params := []any{}
	for _, p := range r.Params {
		kind := map[Variadic]string{Singular: "singular", Plus: "plus", Star: "star"}[p.Variadic]
		params = append(params, map[string]any{"name": p.Name.Text, "kind": kind, "export": p.Export, "default": d.sexp(p.Default)})
	}
	deps := []any{}
	for _, dep := range slices.Concat(r.Deps, r.Subsequent) {
		args := []any{}
		for _, a := range dep.Args {
			args = append(args, d.sexp(a))
		}
		deps = append(deps, map[string]any{"recipe": dep.Target.String(), "arguments": args})
	}
	_, script := attributeNamed(r.Attributes, "script")
	return map[string]any{
		"attributes":   names,
		"doc":          doc,
		"quiet":        r.Quiet,
		"shebang":      r.Shebang || script,
		"private":      private || strings.HasPrefix(r.Name.Text, "_"),
		"priors":       len(r.Deps),
		"parameters":   params,
		"dependencies": deps,
		"body":         dumpBody(f, r),
	}
}

func dumpBody(f *File, r *Recipe) []any {
	d := dumper{text: f.Text}
	out := []any{}
	for _, l := range r.Body {
		parts := []any{}
		text := f.Text[l.Indent.Start+min(len(r.Indent), l.Indent.End-l.Indent.Start) : l.Indent.End]
		for _, frag := range l.Fragments {
			if frag.Kind != FragmentInterpolation {
				text += strings.ReplaceAll(f.Text[frag.Span.Start:frag.Span.End], "\n"+r.Indent, "\n")
				continue
			}
			if text != "" {
				parts = append(parts, text)
				text = ""
			}
			parts = append(parts, []any{d.sexp(frag.Interp)})
		}
		if text != "" {
			parts = append(parts, text)
		}
		out = append(out, parts)
	}
	return out
}

var binaryNames = map[string]string{"+": "concatenate", "/": "join", "++": "list-concatenate", "&&": "and", "||": "or"}

type dumper struct{ text string }

func (d dumper) sexp(e *Expr) any {
	if e == nil {
		return nil
	}
	switch e.Kind {
	case ExprString:
		if e.Str.Prefix == 'f' {
			return d.formatParts(e)
		}
		return e.Str.Value
	case ExprBacktick:
		value := e.Str.Value
		if e.Str.Indented() {
			value = unindent(value)
		}
		return []any{"evaluate", value}
	case ExprVariable:
		return []any{"variable", e.Name.Text}
	case ExprCall:
		if e.Name.Text == "assert" {
			return append([]any{"assert"}, d.sexps(e.Args)...)
		}
		return append([]any{"call", e.Name.Text}, d.sexps(e.Args)...)
	case ExprBinary:
		op := e.Op
		if name, ok := binaryNames[op]; ok {
			op = name
		}
		return []any{op, d.sexp(e.Args[0]), d.sexp(e.Args[1])}
	case ExprNot:
		return []any{"not", d.sexp(e.Args[0])}
	case ExprRoot:
		return []any{"join", nil, d.sexp(e.Args[0])}
	case ExprIf:
		out := []any{"if", d.sexp(e.Args[0]), d.sexp(e.Args[1])}
		if len(e.Args) > 2 {
			return append(out, d.sexp(e.Args[2]))
		}
		return append(out, nil)
	case ExprList:
		return append([]any{"list"}, d.sexps(e.Args)...)
	case ExprGroup:
		return d.sexp(e.Args[0])
	case ExprBad:
	}
	return nil
}

func (d dumper) sexps(es []*Expr) []any {
	out := make([]any, len(es))
	for i, e := range es {
		out[i] = d.sexp(e)
	}
	return out
}

func (d dumper) formatParts(e *Expr) []any {
	out := []any{"format"}
	text := ""
	for _, seg := range e.Str.Segments {
		switch seg.Kind {
		case FragmentText:
			piece, _ := cook(d.text[seg.Span.Start:seg.Span.End], e.Str.Quote[0] == '"', false)
			text += piece
		case FragmentEscape:
			text += "{{"
		case FragmentInterpolation:
			out = append(out, text, d.sexp(seg.Interp))
			text = ""
		}
	}
	return append(out, text)
}
