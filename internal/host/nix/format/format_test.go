package format

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

var update = flag.Bool("update", false, "rewrite the golden files from what the real nixfmt prints")

const (
	referenceTool        = "nixfmt"
	referenceVersion     = "nixfmt 1.5.0"
	repositoryRoot       = "../../../.."
	hostileBudget        = 10 * time.Second
	hostileDepth         = 1500
	parserDepthLimit     = 4000
	allocPerInput        = 8192
	allocPerOutput       = 64
	allocBase            = 4 << 20
	growthPermitted      = 2.5
	fuzzInputLimit       = 1 << 16
	allocPerByteOrdinary = 1000
)

var (
	casesInput   = filepath.Join("testdata", "cases.txt")
	casesGolden  = filepath.Join("testdata", "cases.golden")
	unstableList = filepath.Join("testdata", "not-idempotent.txt")
	repoGoldens  = filepath.Join("testdata", "repo")
)

func repositoryFiles(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{"flake.nix", "nix/*.nix", "tools/dockerfile-oracle/package.nix", "internal/nixeval/eval.nix"} {
		found, err := filepath.Glob(filepath.Join(repositoryRoot, pattern))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, found...)
	}
	corpus := filepath.Join(repositoryRoot, "corpus", "nix")
	err := filepath.WalkDir(corpus, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".nix") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func repositoryGolden(t testing.TB, path string) string {
	t.Helper()
	rel, err := filepath.Rel(repositoryRoot, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(repoGoldens, rel+".golden")
}

func readFile(t testing.TB, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeAtomically(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		t.Fatal(err)
	}
}

func referenceFormat(t testing.TB, src string) string {
	t.Helper()
	cmd := exec.Command(referenceTool, "-q", "-")
	cmd.Stdin = strings.NewReader(src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s refused %q: %v: %s", referenceTool, src, err, stderr.String())
	}
	return string(out)
}

func regenerateCases(t *testing.T) {
	t.Helper()
	var golden strings.Builder
	var unstable []string
	for _, c := range hosttest.Sections(t, casesInput) {
		out := referenceFormat(t, c.Text)
		fmt.Fprintf(&golden, "-- %s --\n%s", c.Name, out)
		if referenceFormat(t, out) != out {
			unstable = append(unstable, c.Name)
		}
	}
	writeAtomically(t, casesGolden, golden.String())
	listing := strings.Join(unstable, "\n")
	if listing != "" {
		listing += "\n"
	}
	writeAtomically(t, unstableList, listing)
}

func regenerateRepository(t *testing.T) {
	t.Helper()
	for _, path := range repositoryFiles(t) {
		writeAtomically(t, repositoryGolden(t, path), referenceFormat(t, readFile(t, path)))
	}
}

func TestCases(t *testing.T) {
	if *update {
		hosttest.RequireTool(t, referenceTool, referenceVersion)
		regenerateCases(t)
	}
	inputs := hosttest.Sections(t, casesInput)
	goldens := hosttest.Sections(t, casesGolden)
	if len(inputs) != len(goldens) {
		t.Fatalf("%d inputs but %d goldens", len(inputs), len(goldens))
	}
	unstable := strings.Fields(readFile(t, unstableList))
	for i, input := range inputs {
		t.Run(input.Name, func(t *testing.T) {
			assertFormats(t, input.Text, goldens[i].Text, !slices.Contains(unstable, input.Name))
		})
	}
}

func TestRepositoryFiles(t *testing.T) {
	if *update {
		hosttest.RequireTool(t, referenceTool, referenceVersion)
		regenerateRepository(t)
	}
	for _, path := range repositoryFiles(t) {
		t.Run(path, func(t *testing.T) {
			assertFormats(t, readFile(t, path), readFile(t, repositoryGolden(t, path)), true)
		})
	}
}

func assertFormats(t *testing.T, input, golden string, stable bool) {
	t.Helper()
	got, err := Format(input)
	if err != nil {
		t.Fatalf("Format(%q): %v", input, err)
	}
	if got != golden {
		t.Errorf("Format(%q)\n got: %q\nwant: %q", input, got, golden)
	}
	assertPreserved(t, input, got)
	if !stable {
		return
	}
	again, err := Format(got)
	if err != nil {
		t.Fatalf("Format of its own output %q: %v", got, err)
	}
	if again != got {
		t.Errorf("Format is not idempotent on %q\nonce: %q\ntwice: %q", input, got, again)
	}
}

func assertPreserved(t testing.TB, input, output string) {
	t.Helper()
	if problem := preservationProblem(t, input, output); problem != "" {
		t.Error(problem)
	}
}

func preservationProblem(t testing.TB, input, output string) string {
	t.Helper()
	if file := syntax.Parse(output); len(file.Errors) > file.BareOr {
		return fmt.Sprintf("formatting %q gave %q, which does not parse: %v", input, output, file.Errors[0])
	}
	if want, have := shape(t, input), shape(t, output); want != have && !selectsFromPath(input) {
		return fmt.Sprintf("formatting %q changed the tree\n got: %s\nwant: %s", input, have, want)
	}
	for _, note := range commentLines(t, input) {
		if !strings.Contains(output, note) {
			return fmt.Sprintf("formatting %q lost the comment text %q from\n%s", input, note, output)
		}
	}
	return ""
}

func selectsFromPath(src string) bool {
	found := false
	syntax.Inspect(syntax.Parse(src).Root, func(n syntax.Node) bool {
		if s, ok := n.(*syntax.Select); ok {
			if p, isPath := s.Target.(*syntax.Path); isPath && p.Kind != syntax.SearchPath {
				found = true
			}
		}
		return !found
	})
	return found
}

func commentLines(t testing.TB, src string) []string {
	t.Helper()
	file := syntax.Parse(src)
	var out []string
	for _, c := range file.Comments {
		body := strings.TrimLeft(src[c.Span.Start:c.Span.End], "#/*")
		body = strings.TrimRight(body, "/*")
		for line := range strings.SplitSeq(body, "\n") {
			if trimmed := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "*")); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func shape(t testing.TB, src string) string {
	t.Helper()
	file := syntax.Parse(src)
	if len(file.Errors) > file.BareOr {
		t.Fatalf("parsing %q: %v", src, file.Errors[0])
	}
	var sb strings.Builder
	var walk func(syntax.Node)
	walk = func(n syntax.Node) {
		switch n := n.(type) {
		case *syntax.Ident:
			fmt.Fprintf(&sb, "id:%s ", n.Name)
		case *syntax.Int:
			fmt.Fprintf(&sb, "int:%s ", n.Text)
		case *syntax.Float:
			fmt.Fprintf(&sb, "float:%s ", n.Text)
		case *syntax.URI:
			sb.WriteString("string() ")
			return
		case *syntax.String:
			sb.WriteString("string(")
			for _, p := range n.Parts {
				if in, ok := p.(*syntax.Interpolation); ok {
					walk(in.Value)
				}
			}
			sb.WriteString(") ")
			return
		case *syntax.Binary:
			fmt.Fprintf(&sb, "op:%s ", n.Op)
		case *syntax.IdentAttr:
			fmt.Fprintf(&sb, "attr:%s ", n.Name)
		default:
			fmt.Fprintf(&sb, "%T ", n)
		}
		first := true
		syntax.Inspect(n, func(child syntax.Node) bool {
			if first {
				first = false
				return true
			}
			walk(child)
			return false
		})
	}
	walk(file.Root)
	return sb.String()
}

func TestRefusesSyntaxErrors(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		src          string
		line, column int
	}{
		{"{ a = ; }\n", 1, 7},
		{"{\n  a = 1;\n  b =\n}\n", 4, 1},
		{"[ 1\n", 1, 1},
		{"\"é ${\n", 1, 1},
	} {
		out, err := Format(c.src)
		if err == nil {
			t.Errorf("Format(%q) = %q, want a refusal", c.src, out)
			continue
		}
		refused, ok := err.(*RefusalError) //nolint:errorlint // Format returns the refusal itself, never a wrapped one
		if !ok {
			t.Errorf("Format(%q) failed with %T: %v", c.src, err, err)
			continue
		}
		if refused.Line != c.line || refused.Column != c.column || out != "" {
			t.Errorf("Format(%q) = %q, refused at %d:%d (%v), want %d:%d", c.src, out, refused.Line, refused.Column, err, c.line, c.column)
		}
		if !strings.Contains(err.Error(), "format") {
			t.Errorf("the refusal %q does not say what was attempted", err)
		}
	}
}

func TestShellInStringsStaysVerbatim(t *testing.T) {
	t.Parallel()

	src := "{\n  buildPhase = ''\n    if   true ;then\n      echo   $out\n    fi\n  '';\n}\n"
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("Format changed the shell in a string: %q", got)
	}
}

func TestCommentsInEveryPositionFormat(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"a # c\n.b\n",
		"a.b # c\nor d\n",
		"a. /* c */ b\n",
		"! # c\na\n",
		"- /* c */ a\n",
		"x # c\n: y\n",
		"{ a } # c\n: y\n",
		"x @ # c\n{ a }: y\n",
		"{ a } /* c */ @ x: y\n",
		"f # c\nx\n",
		"( # c\na)\n",
		"{ # c\na = 1; }\n",
		"a ? # c\nb\n",
		"[ # c\n]\n",
		"\"${ # c\na}\"\n",
		"rec # c\n{ }\n",
	} {
		out, err := Format(src)
		if err != nil {
			t.Errorf("Format(%q): %v", src, err)
			continue
		}
		assertPreserved(t, src, out)
	}
}

type hostileShape struct {
	name, open, closing, core string
}

func (h hostileShape) at(depth int) string {
	return strings.Repeat(h.open, depth) + h.core + strings.Repeat(h.closing, depth)
}

func hostileShapes() []hostileShape {
	long := strings.Repeat("x", 120)
	return []hostileShape{
		{"applications", "f (", ")", "x"},
		{"two arguments", "f y (", ") z", "x"},
		{"last sets", "f { a = ", "; }", "x"},
		{"lists", "[ ", " ]", long},
		{"lambdas", "f (x: ", ")", long},
		{"interpolations", `"${`, `}"`, long},
		{"inner interpolations", `"a${`, `}b"`, long},
		{"operators", "(a + ", ")", long},
		{"or fallbacks", "a.b or (", ")", long},
		{"priorities", "g (h) (", ") (k)", long},
		{"many priorities", "g (h i) [ j ] (", ") { k = 1; }", long},
		{"with lists", "(with a; [ ", " ])", long},
		{"negations", "!", "", "a [\n1] b"},
		{"parens around a set", "(", ")", "{ a = 1; }"},
		{"parens around a string", "(", ")", "''\n  a\n''"},
		{"parens in interpolations", "\"a${(", ")}b\"", "{ a = 1; }"},
		{"lambda chains", "x: { y }: ", "", "{ a = 1; }"},
		{"selections in arguments", "f (a b.", ") c", "d"},
		{"long selections in arguments", "f (a b.${", "}) c", long},
	}
}

type formatCost struct {
	took   time.Duration
	alloc  uint64
	input  int
	output int
}

func (c formatCost) perUnit() float64 {
	return float64(c.alloc) / float64(c.input+c.output/allocPerOutput+1)
}

func measureFormat(t *testing.T, src string) formatCost {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	out, err := Format(src)
	took := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("Format of %d bytes: %v", len(src), err)
	}
	return formatCost{took: took, alloc: after.TotalAlloc - before.TotalAlloc, input: len(src), output: len(out)}
}

func allocBudget(input, output int) uint64 {
	return uint64(allocBase + allocPerInput*input + allocPerOutput*output)
}

func deepestAccepted(h hostileShape) int {
	low, high := 1, parserDepthLimit
	for low < high {
		middle := (low + high + 1) / 2
		if len(syntax.Parse(h.at(middle)).Errors) == 0 {
			low = middle
			continue
		}
		high = middle - 1
	}
	return low
}

func TestHostileNestingStaysFast(t *testing.T) {
	for _, h := range hostileShapes() {
		t.Run(h.name, func(t *testing.T) {
			deepest := deepestAccepted(h)
			var previous formatCost
			for _, depth := range []int{50, 100, 200, 400, 800, hostileDepth, deepest} {
				depth = min(depth, deepest)
				cost := measureFormat(t, h.at(depth))
				t.Logf("depth %d: %d bytes in, %d out, %s, %d allocated", depth, cost.input, cost.output, cost.took, cost.alloc)
				if budget := allocBudget(cost.input, cost.output); cost.alloc > budget {
					t.Fatalf("depth %d allocated %d bytes, over the budget of %d", depth, cost.alloc, budget)
				}
				if cost.took > hostileBudget {
					t.Fatalf("depth %d took %s, over the budget of %s", depth, cost.took, hostileBudget)
				}
				if previous.alloc > allocBase && cost.perUnit() > growthPermitted*previous.perUnit() {
					t.Fatalf("allocation grows faster than the input at depth %d: %.0f bytes per unit after %.0f", depth, cost.perUnit(), previous.perUnit())
				}
				previous = cost
			}
		})
	}
}

func TestDeeperThanTheParserLimitIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := Format(hostileShapes()[0].at(parserDepthLimit)); err == nil {
		t.Error("nesting past the parser limit was formatted instead of refused")
	}
}

func TestAllocationIsLinearInFileSize(t *testing.T) {
	const unit = "  a.b.c = f x { y = [ 1 2 ]; z = \"s${t}\"; }; # note\n"
	small := measureFormat(t, "{\n"+strings.Repeat(unit, 1000)+"}\n")
	large := measureFormat(t, "{\n"+strings.Repeat(unit, 16000)+"}\n")
	if large.perUnit() > growthPermitted*small.perUnit() {
		t.Errorf("allocation per byte rose from %.0f to %.0f as the file grew", small.perUnit(), large.perUnit())
	}
	if perByte := large.alloc / uint64(large.input); perByte > allocPerByteOrdinary {
		t.Errorf("an ordinary file allocates %d bytes per input byte, over %d", perByte, allocPerByteOrdinary)
	}
}

func TestAgreesWithLiveNixfmt(t *testing.T) {
	hosttest.LiveTool(t, referenceTool, referenceVersion)
	for _, path := range repositoryFiles(t) {
		assertAgrees(t, path, readFile(t, path))
	}
	for _, input := range hosttest.Sections(t, casesInput) {
		assertAgrees(t, input.Name, input.Text)
	}
}

func assertAgrees(t *testing.T, name, src string) {
	t.Helper()
	want := referenceFormat(t, src)
	got, err := Format(src)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return
	}
	if got != want {
		t.Errorf("%s differs from nixfmt\n got: %q\nwant: %q", name, got, want)
	}
}

func TestACarriageReturnInsideACommentKeepsTheCommentABlock(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"leading a list", "[/*0\r0\x0e*/]0\n", "[\n  /*\n    0\r0\x0e\n  */\n]\n  0\n"},
		{"trailing a with", "with 0/*0\r0*/;0\n", "with 0\n/*\n  0\r0\n*/\n;\n0\n"},
		{"trailing an expression", "1 /*a\rb*/\n", "1\n/*\n  a\rb\n*/\n"},
		{"one row of a block", "/*\n  a\rb\n*/\n1\n", "/*\n  a\rb\n*/\n1\n"},
		{"at the edge of a comment, where it is only space", "1 /*a\r*/\n", "1 # a\n"},
		{"ending a block's lines", "/*a\r\n*/ 1\n", "# a\n1\n"},
	} {
		got, err := Format(tt.src)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: Format(%q) = %q, want %q", tt.name, tt.src, got, tt.want)
		}
		if problem := preservationProblem(t, tt.src, got); problem != "" {
			t.Errorf("%s: %s", tt.name, problem)
		}
		if again, err := Format(got); err != nil || again != got {
			t.Errorf("%s: formatting %q again gave %q, %v", tt.name, got, again, err)
		}
	}
}

func TestNegatingAPathKeepsItsSpace(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]string{
		"- /0\n":    "- /0\n",
		"- a/b\n":   "- a/b\n",
		"- ./x y\n": "- ./x y\n",
		"- - /0\n":  "-- /0\n",
		"- a\n":     "-a\n",
		"! /0\n":    "!/0\n",
	} {
		got, err := Format(src)
		if err != nil {
			t.Errorf("Format(%q): %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("Format(%q) = %q, want %q", src, got, want)
		}
		if problem := preservationProblem(t, src, got); problem != "" {
			t.Error(problem)
		}
	}
}

func FuzzFormat(f *testing.F) {
	for _, c := range hosttest.Sections(f, casesInput) {
		f.Add(c.Text)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > fuzzInputLimit {
			t.Skip("inputs over the limit say nothing the smaller ones do not")
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		out, err := Format(src)
		runtime.ReadMemStats(&after)
		if used, budget := after.TotalAlloc-before.TotalAlloc, allocBudget(len(src), len(out)); used > budget {
			t.Errorf("Format(%q) allocated %d bytes, over the budget of %d", src, used, budget)
		}
		if err != nil {
			if out != "" {
				t.Errorf("Format(%q) returned both %q and %v", src, out, err)
			}
			return
		}
		problem := preservationProblem(t, src, out)
		if problem == "" {
			again, err := Format(out)
			switch {
			case err != nil:
				problem = fmt.Sprintf("Format(%q) = %q, which does not format again: %v", src, out, err)
			case again != out:
				problem = fmt.Sprintf("Format is not idempotent on %q\nonce: %q\ntwice: %q", src, out, again)
			}
		}
		if problem != "" {
			t.Error(problem)
		}
	})
}
