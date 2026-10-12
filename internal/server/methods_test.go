package server

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

var updateDocs = flag.Bool("update", false, "rewrite docs/lsp-methods.md from the method table")

func advertised(t *testing.T) map[string]any {
	t.Helper()

	srv := New(jsonrpc.NewConn(nil, nil), "test", CurrentProcess())
	result, err := srv.requests["initialize"](t.Context(), nil, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handleInitialize() error = %v", err)
	}
	raw, err := json.Marshal(result.(InitializeResult).Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	var caps map[string]any
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	return caps
}

func lookup(caps map[string]any, path string) any {
	var current any = caps
	for key := range strings.SplitSeq(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}

func truthy(v any) bool {
	switch value := v.(type) {
	case nil:
		return false
	case bool:
		return value
	case float64:
		return value != 0
	case string:
		return value != ""
	case []any:
		return len(value) > 0
	}
	return true
}

func TestMethodTableMatchesHandlers(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test", CurrentProcess())
	table := map[string]method{}
	for _, m := range methods {
		if _, dup := table[m.name]; dup {
			t.Errorf("method %s listed twice", m.name)
		}
		table[m.name] = m
	}
	for name := range srv.requests {
		if m, ok := table[name]; !ok || m.direction != clientRequest || m.support != supported {
			t.Errorf("request handler %s is not listed as an implemented client request: %+v", name, m)
		}
	}
	for name := range srv.notifications {
		if m, ok := table[name]; !ok || m.direction != clientNotification || m.support != supported {
			t.Errorf("notification handler %s is not listed as an implemented client notification: %+v", name, m)
		}
	}
	for _, m := range methods {
		var handled bool
		switch m.direction {
		case clientRequest:
			_, handled = srv.requests[m.name]
		case clientNotification:
			_, handled = srv.notifications[m.name]
			handled = handled || m.name == exitMethod
		default:
			continue
		}
		if handled != (m.support == supported) {
			t.Errorf("%s: support %s but handler registered = %t", m.name, supportNames[m.support], handled)
		}
	}
}

func TestAdvertisedCapabilitiesMatchMethodTable(t *testing.T) {
	t.Parallel()

	caps := advertised(t)
	for _, m := range methods {
		if (m.capability.advertise != nil) != (m.support == supported && m.capability.path != "") {
			t.Errorf("%s: support %s with capability %q, but advertises it = %t", m.name, supportNames[m.support], m.capability.path, m.capability.advertise != nil)
		}
		if m.capability.path == "" {
			continue
		}
		got := truthy(lookup(caps, m.capability.path))
		switch m.support {
		case supported:
			if !got {
				t.Errorf("%s is implemented but %s is not advertised", m.name, m.capability.path)
			}
		case notAdvertised:
			if got {
				t.Errorf("%s is not implemented but %s is advertised", m.name, m.capability.path)
			}
		case sent, notSent:
		}
	}
}

func renderMethodTable() string {
	var sb strings.Builder
	sb.WriteString("# LSP method coverage\n\n")
	sb.WriteString("Generated from `internal/server/methods.go` by `go test ./internal/server -run TestMethodDocs -update`.\n")
	sb.WriteString("The conformance tests in `methods_test.go` check that every implemented method has a handler and an\n")
	sb.WriteString("advertised capability, and that nothing else is handled or advertised.\n")
	for _, d := range []direction{clientRequest, clientNotification, serverRequest, serverNotification} {
		fmt.Fprintf(&sb, "\n## %s\n\n| Method | Status | Capability | Notes |\n| --- | --- | --- | --- |\n", strings.ToUpper(directionNames[d][:1])+directionNames[d][1:]+"s")
		for _, m := range methods {
			if m.direction != d {
				continue
			}
			capability := ""
			if m.capability.path != "" {
				capability = "`" + m.capability.path + "`"
			}
			fmt.Fprintf(&sb, "| `%s` | %s | %s | %s |\n", m.name, supportNames[m.support], capability, m.note)
		}
	}
	return sb.String()
}

func TestMethodDocs(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "docs", "lsp-methods.md")
	want := renderMethodTable()
	if *updateDocs {
		if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (regenerate with -update): %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s is out of date; run go test ./internal/server -run TestMethodDocs -update", path)
	}
}
