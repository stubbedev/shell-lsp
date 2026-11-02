package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

const (
	fakeServerEnv     = "SHELL_LSP_NIXPARITY_FAKE_SERVER"
	fakeCrashStatus   = 3
	fakeServerTimeout = time.Second
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeServerEnv) != "" {
		serveFake()
		return
	}
	os.Exit(m.Run())
}

func serveFake() {
	conn := jsonrpc.NewConn(os.Stdin, os.Stdout)
	for {
		msg, err := conn.Read()
		if err != nil {
			return
		}
		switch msg.Method {
		case "initialize":
			_ = conn.Reply(msg.ID, map[string]any{
				"capabilities": map[string]any{"hoverProvider": true, "definitionProvider": true, "referencesProvider": true, "renameProvider": false},
			}, nil)
		case "textDocument/hover", "shutdown":
			_ = conn.Reply(msg.ID, nil, nil)
		case "textDocument/references":
			os.Exit(fakeCrashStatus)
		case "exit":
			return
		}
	}
}

func fakeSession(t *testing.T) *session {
	t.Helper()

	t.Setenv(fakeServerEnv, "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	doc := docstore.NewDocument(fileuri.FromPath(filepath.Join(root, "a.nix")), languageID, 1, "abc\n")
	s := &session{
		cfg:  serverConfig{name: "fake", argv: []string{os.Args[0]}, root: root, timeout: fakeServerTimeout},
		norm: normalizer{root: root},
		docs: []document{{rel: "a.nix", doc: doc}},
	}
	if err := s.launch(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.retire)
	return s
}

func TestSessionReportsWhatTheServerDid(t *testing.T) {
	s := fakeSession(t)
	ctx := t.Context()
	steps := []struct {
		name     string
		feature  feature
		want     outcome
		restarts int
	}{
		{"a null answer is empty", featureHover, outcomeEmpty, 0},
		{"a capability it lacks is not asked", featureFormatting, outcomeUnsupported, 0},
		{"a capability it declares false is not asked", featureRename, outcomeUnsupported, 0},
		{"a request it never answers times out", featureDefinition, outcomeTimeout, 0},
		{"it still answers after a timeout", featureHover, outcomeEmpty, 0},
		{"a request it dies on is a crash", featureReferences, outcomeCrashed, 0},
		{"the next request restarts it", featureHover, outcomeEmpty, 1},
	}
	for _, step := range steps {
		got := s.ask(ctx, query{feature: step.feature})
		if got.outcome != step.want || s.restarts != step.restarts {
			t.Fatalf("%s: outcome %d after %d restarts, want %d after %d", step.name, got.outcome, s.restarts, step.want, step.restarts)
		}
	}
}

func TestSessionGivesUpAfterItsRestarts(t *testing.T) {
	s := fakeSession(t)
	ctx := t.Context()
	s.restarts = maxRestarts
	if got := s.ask(ctx, query{feature: featureReferences}); got.outcome != outcomeCrashed {
		t.Fatalf("outcome %d, want a crash", got.outcome)
	}
	if got := s.ask(ctx, query{feature: featureHover}); got.outcome != outcomeCrashed {
		t.Errorf("outcome %d with no restarts left, want a crash and never an empty answer", got.outcome)
	}
}

func TestSessionRestartsAWedgedServer(t *testing.T) {
	s := fakeSession(t)
	s.timeouts = wedgedAfterTimeout
	first := s.srv
	if got := s.ask(t.Context(), query{feature: featureHover}); got.outcome != outcomeEmpty {
		t.Fatalf("outcome %d after the restart, want empty", got.outcome)
	}
	if s.restarts != 1 || s.srv == first {
		t.Errorf("%d restarts, want the wedged server replaced", s.restarts)
	}
	if first.cmd.ProcessState == nil {
		t.Error("the replaced server was not reaped")
	}
}

func TestRetireReapsTheServer(t *testing.T) {
	s := fakeSession(t)
	srv := s.srv
	s.retire()
	if srv.cmd.ProcessState == nil {
		t.Error("the server was not reaped")
	}
	if s.srv != nil {
		t.Error("the session still holds the retired server")
	}
}
