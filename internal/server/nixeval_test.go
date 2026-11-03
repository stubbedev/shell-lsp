//go:build unix

package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/nixeval"
)

const nixTreeJSON = `{"options": [
	{"loc": ["services", "nginx", "enable"], "type": "boolean", "description": "Runs nginx.", "default": "false", "example": "true", "declarations": [{"file": "/modules/nginx.nix", "line": 4}]},
	{"loc": ["services", "nginx", "virtualHosts", "<name>", "root"], "type": "path"},
	{"loc": ["home-manager", "users", "<name>", "home", "stateVersion"], "type": "string"}
], "truncated": []}`

const flakePartsTreeJSON = `{"options": [
	{"loc": ["perSystem", "packages"], "type": "lazy attribute set of package", "default": "«not evaluated»"},
	{"loc": ["systems"], "type": "list of string"}
], "truncated": []}`

type fakeEvaluator struct {
	mu       sync.Mutex
	sources  []nixeval.OptionSource
	trees    map[nixeval.OptionSource]string
	attrs    []string
	packages map[string]*nixeval.Package
	err      error
	stale    *nixeval.Stale
	warmed   []string
	closed   int
	realise  error
	realised []string
}

func (f *fakeEvaluator) Warm(dir string) *nixeval.Refresh {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warmed = append(f.warmed, dir)
	refresh, finish := nixeval.NewRefresh()
	finish(nil)
	return refresh
}

func fakeAnswer[T any](f *fakeEvaluator, value T) (nixeval.Answer[T], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nixeval.Answer[T]{}, f.err
	}
	return nixeval.NewAnswer(value, f.stale), nil
}

func (f *fakeEvaluator) OptionSources(context.Context, string) (nixeval.Answer[[]nixeval.OptionSource], error) {
	return fakeAnswer(f, f.sources)
}

func (f *fakeEvaluator) Options(_ context.Context, _ string, source nixeval.OptionSource) (nixeval.Answer[*nixeval.OptionTree], error) {
	tree, err := nixeval.ParseOptionTree([]byte(f.trees[source]))
	if err != nil {
		return nixeval.Answer[*nixeval.OptionTree]{}, err
	}
	return fakeAnswer(f, tree)
}

func (f *fakeEvaluator) BaseOptions(context.Context, string) (nixeval.Answer[*nixeval.OptionTree], error) {
	tree, err := nixeval.ParseOptionTree([]byte(nixTreeJSON))
	if err != nil {
		return nixeval.Answer[*nixeval.OptionTree]{}, err
	}
	return fakeAnswer(f, tree)
}

func (f *fakeEvaluator) NixpkgsAttrs(_ context.Context, _ string, _ []string, prefix string) (nixeval.Answer[[]string], error) {
	return fakeAnswer(f, slices.DeleteFunc(slices.Clone(f.attrs), func(name string) bool { return !strings.HasPrefix(name, prefix) }))
}

func (f *fakeEvaluator) NixpkgsPackage(_ context.Context, _ string, path []string) (nixeval.Answer[*nixeval.Package], error) {
	return fakeAnswer(f, f.packages[strings.Join(path, ".")])
}

func (f *fakeEvaluator) RealiseFile(_ context.Context, _, file string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.realised = append(f.realised, file)
	return f.realise
}

func (f *fakeEvaluator) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
}

func (f *fakeEvaluator) set(update func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	update()
}

func (f *fakeEvaluator) closes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func newFakeEvaluator() *fakeEvaluator {
	box := nixeval.OptionSource{Kind: nixeval.SourceNixOS, Name: "box"}
	parts := nixeval.OptionSource{Kind: nixeval.SourceFlakeParts}
	return &fakeEvaluator{
		sources:  []nixeval.OptionSource{box, parts},
		trees:    map[nixeval.OptionSource]string{box: nixTreeJSON, parts: flakePartsTreeJSON},
		attrs:    []string{"hello", "world"},
		packages: map[string]*nixeval.Package{"hello": {Name: "hello-2.12", Version: "2.12", Description: "Says hello."}},
	}
}

type nixEvalProject struct {
	c     *testClient
	srv   *Server
	root  string
	opens *atomic.Int32
}

func openNixEvalProject(t *testing.T, enabled bool, fake *fakeEvaluator, files map[string]string) *nixEvalProject {
	t.Helper()
	return openNixEvalProjectPatient(t, enabled, fake, files, clientTimeout)
}

func openNixEvalProjectPatient(t *testing.T, enabled bool, fake *fakeEvaluator, files map[string]string, patience time.Duration) *nixEvalProject {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	opens := new(atomic.Int32)
	options, err := json.Marshal(map[string]map[string]bool{"nix": {"evaluate": enabled}})
	if err != nil {
		t.Fatal(err)
	}
	c, srv := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "initializationOptions": `+string(options)+`, `+nativeCapabilities+`}`, func(s *Server) {
		s.nixEval.classPatience = patience
		s.nixEval.open = func(func(error)) (nixEvaluator, error) {
			opens.Add(1)
			if fake == nil {
				return nil, errors.New("the evaluator must not open")
			}
			return fake, nil
		}
	})
	c.notify("initialized", `{}`)
	return &nixEvalProject{c: c, srv: srv, root: root, opens: opens}
}

func (p *nixEvalProject) open(t *testing.T, name, marked string) string {
	t.Helper()
	text, line, character := markedPosition(t, marked)
	uri := fileuri.FromPath(filepath.Join(p.root, name))
	p.c.notify("textDocument/didOpen", openLanguageParams(uri, "", text))
	return positionParams(uri, line, character, "")
}

func (p *nixEvalProject) labels(t *testing.T, params string) (lsp.CompletionList, []string) {
	t.Helper()
	list := decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", params))
	labels := make([]string, len(list.Items))
	for i, it := range list.Items {
		labels[i] = it.Label
	}
	return list, labels
}

const emptyFlake = "{ outputs = _: { }; }\n"

func TestNixEvaluationOffOpensNothing(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "spawned")
	bin := t.TempDir()
	script := "#!/bin/sh\n: > '" + marker + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := openNixEvalProject(t, false, nil, map[string]string{"flake.nix": emptyFlake})
	params := p.open(t, "web.nix", "{ pkgs, ... }: { services.| x = pkgs.hello; }")
	if _, labels := p.labels(t, params); slices.Contains(labels, "nginx") {
		t.Errorf("completion with evaluation off = %q", labels)
	}
	hover := p.open(t, "other.nix", "{ pkgs, ... }: pkgs.hel|lo")
	if msg := p.c.call("textDocument/hover", hover); msg.Error != nil {
		t.Errorf("hover with evaluation off: %v", msg.Error)
	}
	if got := p.opens.Load(); got != 0 {
		t.Errorf("the evaluator opened %d times with the setting off", got)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("nix ran with the setting off: %v", err)
	}
}

func TestNixEvaluationStaysOffOutsideAFlake(t *testing.T) {
	t.Parallel()

	p := openNixEvalProject(t, true, newFakeEvaluator(), map[string]string{"web.nix": ""})
	params := p.open(t, "web.nix", "{ services.| }")
	_, labels := p.labels(t, params)
	if slices.Contains(labels, "nginx") || p.opens.Load() != 0 {
		t.Errorf("no flake: labels %q, %d opens", labels, p.opens.Load())
	}
}

func TestNixCompletionOfOptions(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	tests := []struct {
		name, src string
		want      []string
		missing   []string
	}{
		{"ambiguous.nix", "{ services.nginx.| }", []string{"enable", "virtualHosts"}, nil},
		{"union.nix", "{ | }", []string{"services", "perSystem", "systems", "home"}, nil},
		{"per-system.nix", "{ perSystem = { ... }: { | }; }", []string{"packages"}, []string{"services"}},
		{"dendritic.nix", "{ flake.modules.nixos.web = { | }; }", []string{"services"}, []string{"perSystem"}},
		{"home.nix", "{ flake.modules.homeManager.web = { | }; }", []string{"home"}, []string{"services", "perSystem"}},
	}
	for _, tt := range tests {
		_, labels := p.labels(t, p.open(t, tt.name, tt.src))
		for _, want := range tt.want {
			if !slices.Contains(labels, want) {
				t.Errorf("%s: %q lacks %s", tt.name, labels, want)
			}
		}
		for _, missing := range tt.missing {
			if slices.Contains(labels, missing) {
				t.Errorf("%s: %q holds %s", tt.name, labels, missing)
			}
		}
	}
	list, _ := p.labels(t, p.open(t, "detail.nix", "{ services.nginx.| }"))
	i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == "enable" })
	if i < 0 || !strings.Contains(list.Items[i].Detail, "boolean") || !strings.Contains(list.Items[i].Detail, "NixOS configuration box") {
		t.Errorf("the enable item is not marked with its type and source: %+v", list.Items)
	}
	if list.IsIncomplete {
		t.Error("a fresh answer is marked incomplete")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !slices.Contains(fake.warmed, p.root) {
		t.Errorf("warmed %q, want the flake %s", fake.warmed, p.root)
	}
}

func TestNixUnionMarksEverySource(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	me := nixeval.OptionSource{Kind: nixeval.SourceHomeManager, Name: "me"}
	fake.sources = append(fake.sources, me)
	fake.trees[me] = nixTreeJSON
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	list, _ := p.labels(t, p.open(t, "both.nix", "{ services.nginx.| }"))
	i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == "enable" })
	if i < 0 || !strings.Contains(list.Items[i].Detail, "NixOS configuration box") || !strings.Contains(list.Items[i].Detail, "home-manager configuration me") {
		t.Errorf("a shared option is not marked with both sources: %+v", list.Items)
	}
}

func TestNixClassWithoutASourceFallsBackToTheLockedNixpkgs(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	fake.sources = nil
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	list, _ := p.labels(t, p.open(t, "web.nix", "{ flake.nixosModules.web = { services.nginx.| }; }"))
	i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == "enable" })
	if i < 0 || !strings.Contains(list.Items[i].Detail, "locked nixpkgs") {
		t.Errorf("items = %+v", list.Items)
	}
}

func TestNixCompletionOfPackages(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	list, labels := p.labels(t, p.open(t, "pkgs.nix", "{ pkgs, ... }: pkgs.|"))
	if !slices.Equal(labels, []string{"hello", "world"}) || list.IsIncomplete {
		t.Fatalf("pkgs. completes as %q, incomplete %t", labels, list.IsIncomplete)
	}
	raw, err := json.Marshal(list.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CompletionItem](t, p.c.call("completionItem/resolve", string(raw)))
	if resolved.Documentation == nil {
		t.Fatalf("resolved item = %+v", resolved)
	}
	if doc, err := json.Marshal(resolved.Documentation); err != nil || !strings.Contains(string(doc), "Says hello.") {
		t.Errorf("resolved documentation = %s, %v", doc, err)
	}
	with, labels := p.labels(t, p.open(t, "with.nix", "{ pkgs, ... }: with pkgs; [ he| ]"))
	if !slices.Contains(labels, "hello") || !with.IsIncomplete {
		t.Errorf("with pkgs completes as %q, incomplete %t: a typed prefix narrows the answer, so it must be asked again", labels, with.IsIncomplete)
	}
}

func TestNixHoverOfOptionsAndPackages(t *testing.T) {
	t.Parallel()

	p := openNixEvalProject(t, true, newFakeEvaluator(), map[string]string{"flake.nix": emptyFlake})
	option := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.open(t, "web.nix", "{ services.nginx.en|able = true; }")))
	if option == nil {
		t.Fatal("no hover on an option")
	}
	for _, want := range []string{"services.nginx.enable : boolean", "Runs nginx.", "Default: `false`", "Example: `true`", "/modules/nginx.nix:4", "NixOS configuration box"} {
		if !strings.Contains(option.Contents.Value, want) {
			t.Errorf("option hover %q lacks %q", option.Contents.Value, want)
		}
	}
	pkg := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.open(t, "pkg.nix", "{ pkgs, ... }: pkgs.hel|lo")))
	if pkg == nil || !strings.Contains(pkg.Contents.Value, "Says hello.") {
		t.Errorf("package hover = %+v", pkg)
	}
}

func TestNixNotReadyAnswersStaticallyAndAsksAgain(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	refresh, finish := nixeval.NewRefresh()
	fake.err = &nixeval.NotReadyError{Refresh: refresh, Err: context.DeadlineExceeded}
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	params := p.open(t, "web.nix", "let local = 1; in { services.| x = l; }")
	list, labels := p.labels(t, params)
	if !list.IsIncomplete || slices.Contains(labels, "nginx") {
		t.Fatalf("while evaluating: %q, incomplete %t", labels, list.IsIncomplete)
	}
	fake.set(func() { fake.err = nil })
	finish(nil)
	list, labels = p.labels(t, params)
	if list.IsIncomplete || !slices.Contains(labels, "nginx") {
		t.Errorf("after the refresh: %q, incomplete %t", labels, list.IsIncomplete)
	}
}

func TestNixStaleAnswerIsUsedAndAskedAgain(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	refresh, finish := nixeval.NewRefresh()
	defer finish(nil)
	fake.stale = &nixeval.Stale{Refresh: refresh}
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	list, labels := p.labels(t, p.open(t, "web.nix", "{ services.| }"))
	if !list.IsIncomplete || !slices.Contains(labels, "nginx") {
		t.Errorf("stale: %q, incomplete %t", labels, list.IsIncomplete)
	}
}

func messagesOf(c *testClient, method string) []*jsonrpc.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(c.messages), func(m *jsonrpc.Message) bool { return m.Method != method })
}

func TestNixRefreshOutcomes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		err   error
		shown bool
	}{
		{"success", nil, false},
		{"superseded", nixeval.ErrSuperseded, false},
		{"failure", errors.New("error: the flake does not evaluate"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeEvaluator()
			refresh, finish := nixeval.NewRefresh()
			fake.stale = &nixeval.Stale{Refresh: refresh}
			p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
			params := p.open(t, "web.nix", "{ services.| }")
			p.labels(t, params)
			finish(tt.err)
			if tt.shown {
				p.c.awaitNotification(showMessageMethod, func(raw json.RawMessage) bool {
					return strings.Contains(string(raw), "does not evaluate")
				})
			}
			for range 5 {
				list, _ := p.labels(t, params)
				if list.IsIncomplete {
					t.Error("a finished refresh keeps the client asking")
				}
			}
			if shown := len(messagesOf(p.c, showMessageMethod)); shown != 0 {
				t.Errorf("%d messages shown beyond the expected one", shown)
			}
			for _, method := range []string{inlayHintRefreshMethod, diagnosticRefreshMethod, semanticTokensRefreshMethod, codeLensRefreshMethod} {
				if got := messagesOf(p.c, method); len(got) != 0 {
					t.Errorf("a refresh that changes none of its results asked for %s", method)
				}
			}
		})
	}
}

func TestNixMissingShowsOneMessageAndStaticFeaturesWork(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	fake.err = &nixeval.NixNotFoundError{Err: errors.New("executable file not found")}
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	params := p.open(t, "web.nix", "let local = 1; in lo|")
	for range 3 {
		if _, labels := p.labels(t, params); !slices.Contains(labels, "local") {
			t.Errorf("static completion is gone: %q", labels)
		}
	}
	p.labels(t, p.open(t, "pkg2.nix", "{ pkgs, ... }: pkgs.|"))
	p.c.awaitNotification(showMessageMethod, func(raw json.RawMessage) bool { return strings.Contains(string(raw), "no nix binary is on PATH") })
	p.labels(t, p.open(t, "pkg3.nix", "{ pkgs, ... }: pkgs.|"))
	if got := len(messagesOf(p.c, showMessageMethod)); got != 0 {
		t.Errorf("%d messages shown after the first", got)
	}
}

func TestNixEvaluatorOpensOnceAndClosesOnShutdown(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	for _, name := range []string{"a.nix", "b.nix"} {
		p.labels(t, p.open(t, name, "{ services.| }"))
	}
	if got := p.opens.Load(); got != 1 {
		t.Errorf("%d opens, want 1", got)
	}
	p.c.shutdownAndExit()
	if got := fake.closes(); got != 1 {
		t.Errorf("Close ran %d times, want once", got)
	}
}

func TestNixEvaluationStopsWhenTurnedOff(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	params := p.open(t, "web.nix", "{ services.| }")
	p.labels(t, params)
	p.c.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"nix": {"evaluate": false}}}}`)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, labels := p.labels(t, params)
		if !slices.Contains(labels, "nginx") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still evaluating after being turned off: %q", labels)
		}
	}
	if got := fake.closes(); got != 1 {
		t.Errorf("Close ran %d times, want once", got)
	}
}

func TestNixRecipeListingCarriesTheFlakeOutputs(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, map[string]string{
		"flake.nix": "{ outputs = { self }: { packages.x86_64-linux.foo = 1; }; }\n",
		"justfile":  "build:\n    true\n",
	}, "flake.nix", "justfile")
	listing := decodeResult[[]recipeListing](t, p.c.call("workspace/executeCommand", `{"command": "`+commandListRecipes+`"}`))
	tools := map[string]string{}
	var flake recipeListing
	for _, entry := range listing {
		tools[entry.Name] = entry.Tool
		if entry.Tool == "nix" {
			flake = entry
		}
	}
	if tools["build"] != "just" || flake.Name != "packages.foo" {
		t.Fatalf("listing = %+v", listing)
	}
	if !slices.Equal(flake.Argv[:2], []string{"nix", "build"}) {
		t.Errorf("the flake output runs as %q", flake.Argv)
	}
}

func TestNixRunRefusalNamesNoSpecificTool(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, map[string]string{"a.bash": "echo\n"}, "a.bash")
	msg := p.c.call("workspace/executeCommand", `{"command": "`+commandRunRecipe+`", "arguments": [{"uri": "`+p.uri("a.bash")+`", "name": "x"}]}`)
	if msg.Error == nil || !strings.Contains(msg.Error.Message, "nothing to run") || strings.Contains(msg.Error.Message, "justfile") {
		t.Errorf("error = %+v", msg.Error)
	}
}

func awaitNixWarms(t *testing.T, p *nixEvalProject, fake *fakeEvaluator, want []string) {
	t.Helper()
	settled := func() bool {
		fake.mu.Lock()
		same := slices.Equal(fake.warmed, want)
		fake.mu.Unlock()
		if !same {
			return false
		}
		session := p.srv.nixSession()
		session.mu.Lock()
		defer session.mu.Unlock()
		return len(session.warming) == 0
	}
	deadline := time.Now().Add(10 * time.Second)
	for !settled() {
		if time.Now().After(deadline) {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			t.Fatalf("warmed %q, want %q", fake.warmed, want)
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !slices.Equal(fake.warmed, want) {
		t.Fatalf("warmed %q after it had settled at %q", fake.warmed, want)
	}
}

func (p *nixEvalProject) save(name string) {
	p.c.notify("textDocument/didSave", `{"textDocument": {"uri": "`+fileuri.FromPath(filepath.Join(p.root, name))+`"}}`)
}

func TestNixOpeningAFileWarmsItsFlakeOnce(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"one/flake.nix": emptyFlake, "two/flake.nix": emptyFlake})
	one, two := filepath.Join(p.root, "one"), filepath.Join(p.root, "two")
	p.open(t, "one/a.nix", "{ |}")
	awaitNixWarms(t, p, fake, []string{one})
	p.open(t, "one/nested/b.nix", "{ |}")
	p.open(t, "one/run.sh", "echo |")
	p.open(t, "outside.nix", "{ |}")
	awaitNixWarms(t, p, fake, []string{one})
	p.open(t, "two/a.nix", "{ |}")
	awaitNixWarms(t, p, fake, []string{one, two})
	if got := p.opens.Load(); got != 1 {
		t.Errorf("the evaluator opened %d times, want once", got)
	}
}

func TestNixSavingAFileWarmsItsFlakeAgain(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"one/flake.nix": emptyFlake})
	one := filepath.Join(p.root, "one")
	p.open(t, "one/a.nix", "{ |}")
	awaitNixWarms(t, p, fake, []string{one})
	p.save("one/a.nix")
	awaitNixWarms(t, p, fake, []string{one, one})
	p.save("one/run.sh")
	p.save("outside.nix")
	awaitNixWarms(t, p, fake, []string{one, one})
	p.save("one/a.nix")
	awaitNixWarms(t, p, fake, []string{one, one, one})
}

func TestNixOpeningAndSavingWarmNothingWithTheSettingOff(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, false, fake, map[string]string{"flake.nix": emptyFlake})
	p.open(t, "a.nix", "{ |}")
	p.save("a.nix")
	p.save("flake.nix")
	time.Sleep(50 * time.Millisecond)
	if got := p.opens.Load(); got != 0 {
		t.Errorf("the evaluator opened %d times with the setting off", got)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.warmed) != 0 {
		t.Errorf("warmed %q with the setting off", fake.warmed)
	}
}

func TestNixTurningTheSettingOffClosesTheSessionAtOnce(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	p.open(t, "a.nix", "{ |}")
	awaitNixWarms(t, p, fake, []string{p.root})
	p.c.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"nix": {"evaluate": false}}}}`)
	deadline := time.Now().Add(10 * time.Second)
	for fake.closes() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("Close ran %d times without any request after the setting turned off, want once", fake.closes())
		}
		time.Sleep(time.Millisecond)
	}
	p.save("a.nix")
	time.Sleep(50 * time.Millisecond)
	if got := p.opens.Load(); got != 1 {
		t.Errorf("the evaluator opened %d times after the setting turned off, want once", got)
	}
	p.c.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"nix": {"evaluate": true}}}}`)
	awaitNixWarms(t, p, fake, []string{p.root, p.root})
	if got := p.opens.Load(); got != 2 {
		t.Errorf("the evaluator opened %d times after the setting turned on again, want twice", got)
	}
}

func TestNixNotReadyAsksAgainEvenIfTheRefreshJustFinished(t *testing.T) {
	t.Parallel()

	fake := newFakeEvaluator()
	refresh, finish := nixeval.NewRefresh()
	finish(nil)
	fake.err = &nixeval.NotReadyError{Refresh: refresh, Err: context.DeadlineExceeded}
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
	list, labels := p.labels(t, p.open(t, "web.nix", "{ services.| }"))
	if !list.IsIncomplete || slices.Contains(labels, "nginx") {
		t.Errorf("a request that got no answer: %q, incomplete %t", labels, list.IsIncomplete)
	}
}

func TestNixRequestDoesNotWaitForTheModuleClassesOfItsFile(t *testing.T) {
	t.Parallel()

	const web = "{ | }"
	p := openNixEvalProjectPatient(t, true, newFakeEvaluator(), map[string]string{
		"flake.nix": "{ outputs = { nixpkgs, ... }: { nixosConfigurations.box = nixpkgs.lib.nixosSystem { modules = [ ./web.nix ]; }; }; }\n",
		"web.nix":   "{ }\n",
	}, time.Millisecond)
	deadline := time.Now().Add(clientTimeout)
	session := p.srv.nixSession()
	for session == nil {
		if time.Now().After(deadline) {
			t.Fatal("the evaluation session never opened")
		}
		time.Sleep(time.Millisecond)
		session = p.srv.nixSession()
	}
	links := &session.classLinks
	links.Lock()
	params := p.open(t, "web.nix", web)
	list, labels := p.labels(t, params)
	links.Unlock()
	if !list.IsIncomplete || !slices.Contains(labels, "services") || !slices.Contains(labels, "systems") {
		t.Fatalf("with the classes unknown: %q, incomplete %t, want every source and to be asked again", labels, list.IsIncomplete)
	}
	settled := func(what string) []string {
		t.Helper()
		deadline := time.Now().Add(clientTimeout)
		for {
			list, labels := p.labels(t, params)
			if !list.IsIncomplete {
				return labels
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: still incomplete: %q", what, labels)
			}
			time.Sleep(time.Millisecond)
		}
	}
	if labels := settled("after the link"); !slices.Contains(labels, "services") || slices.Contains(labels, "systems") {
		t.Fatalf("with the classes known: %q, want only the NixOS source", labels)
	}
	links.Lock()
	defer links.Unlock()
	p.c.notify("textDocument/didChange", `{"textDocument": {"uri": "`+fileuri.FromPath(filepath.Join(p.root, "web.nix"))+`", "version": 2}, "contentChanges": [{"text": "{  }"}]}`)
	list, labels = p.labels(t, params)
	if !list.IsIncomplete || !slices.Contains(labels, "services") || slices.Contains(labels, "systems") {
		t.Errorf("while the classes are found again: %q, incomplete %t, want the earlier classes and to be asked again", labels, list.IsIncomplete)
	}
}

func TestNixAModuleNamedFlakeIsNotAFlake(t *testing.T) {
	t.Parallel()

	const module = "{ perSystem = { ... }: { }; }\n"
	fake := newFakeEvaluator()
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake, "modules/flake.nix": module})
	p.open(t, "modules/a.nix", "{ |}")
	awaitNixWarms(t, p, fake, []string{p.root})

	lone := newFakeEvaluator()
	bare := openNixEvalProject(t, true, lone, map[string]string{"flake.nix": module})
	_, labels := bare.labels(t, bare.open(t, "a.nix", "{ services.| }"))
	if slices.Contains(labels, "nginx") || bare.opens.Load() != 0 {
		t.Errorf("a module named flake.nix alone: labels %q, %d opens", labels, bare.opens.Load())
	}
}

func TestNixJumpNeverOpensAMissingFile(t *testing.T) {
	t.Parallel()

	const file = "/nix/store/missing-source/pkgs/hello.nix"
	refresh, finish := nixeval.NewRefresh()
	t.Cleanup(func() { finish(nil) })
	for _, tt := range []struct {
		name    string
		realise error
		want    int
		notice  string
	}{
		{"on disk", nil, 1, ""},
		{"still fetching", &nixeval.NotReadyError{Refresh: refresh, Err: context.DeadlineExceeded}, 0, "Still fetching"},
		{"unavailable", &nixeval.FileUnavailableError{File: file, Err: errors.New("no network")}, 0, "is not on disk"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeEvaluator{
				sources:  []nixeval.OptionSource{},
				packages: map[string]*nixeval.Package{"hello": {Name: "hello-2.12", Declaration: nixeval.Declaration{File: file, Line: 3}}},
				realise:  tt.realise,
			}
			p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake})
			params := p.open(t, "x.nix", "{ pkgs, ... }: pkgs.hel|lo")
			got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", params))
			if len(got) != tt.want {
				t.Errorf("definition = %+v, want %d locations", got, tt.want)
			}
			if tt.want == 1 && got[0].Range.Start.Line != 2 {
				t.Errorf("definition = %+v, want line index 2", got)
			}
			if tt.notice != "" && !strings.Contains(shownNotices(p), tt.notice) {
				t.Errorf("notices = %q, want %q", shownNotices(p), tt.notice)
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if !slices.Contains(fake.realised, file) {
				t.Errorf("realised = %q, want the declaration", fake.realised)
			}
		})
	}
}

func TestNixUnsupportedVersionIsOneNoticeAcrossFlakes(t *testing.T) {
	t.Parallel()

	fake := &fakeEvaluator{sources: []nixeval.OptionSource{}, err: &nixeval.UnsupportedNixError{Reported: "nix 2.18"}}
	p := openNixEvalProject(t, true, fake, map[string]string{"flake.nix": emptyFlake, "other/flake.nix": emptyFlake})
	for _, name := range []string{"x.nix", "other/x.nix"} {
		p.labels(t, p.open(t, name, "{ flake.| }"))
	}
	if notices := shownNotices(p); strings.Count(notices, "nix --version reports") != 1 {
		t.Errorf("notices = %q", notices)
	}
}
