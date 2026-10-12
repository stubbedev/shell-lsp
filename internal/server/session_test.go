package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

type sessionStep struct {
	line    int
	kind    string
	payload json.RawMessage
}

func loadSession(t *testing.T, path string) []sessionStep {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var steps []sessionStep
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kind, payload, ok := strings.Cut(line, " ")
		if !ok || (kind != "->" && kind != "<-" && kind != "=>") {
			t.Fatalf("%s:%d: expected '-> json', '<- json' or '=> json'", path, n)
		}
		if !json.Valid([]byte(payload)) {
			t.Fatalf("%s:%d: invalid JSON", path, n)
		}
		steps = append(steps, sessionStep{line: n, kind: kind, payload: json.RawMessage(payload)})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return steps
}

func subset(expected, actual any) bool {
	switch e := expected.(type) {
	case map[string]any:
		a, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range e {
			if !subset(value, a[key]) {
				return false
			}
		}
		return true
	case []any:
		a, ok := actual.([]any)
		if !ok || len(a) != len(e) {
			return false
		}
		for i := range e {
			if !subset(e[i], a[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(expected, actual)
}

func TestRecordedSessions(t *testing.T) {
	t.Parallel()

	sessions, err := filepath.Glob(filepath.Join("testdata", "sessions", "*.session"))
	if err != nil || len(sessions) == 0 {
		t.Fatalf("no sessions found: %v", err)
	}
	for _, path := range sessions {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			replay(t, path, loadSession(t, path))
		})
	}
}

func replay(t *testing.T, path string, steps []sessionStep) {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	srv := New(jsonrpc.NewConn(serverReader, serverWriter), "test", CurrentProcess())
	runErr := make(chan error, 1)
	go func() {
		runErr <- srv.Run(t.Context())
		_ = serverWriter.Close()
	}()
	client := jsonrpc.NewConn(clientReader, clientWriter)
	incoming := make(chan *jsonrpc.Message, 64)
	go func() {
		defer close(incoming)
		for {
			msg, err := client.Read()
			if err != nil {
				return
			}
			incoming <- msg
		}
	}()

	var lastServerRequest json.RawMessage
	for _, step := range steps {
		where := fmt.Sprintf("%s:%d", path, step.line)
		switch step.kind {
		case "->":
			var msg jsonrpc.Message
			if err := json.Unmarshal(step.payload, &msg); err != nil {
				t.Fatalf("%s: %v", where, err)
			}
			if err := client.Write(&msg); err != nil {
				t.Fatalf("%s: writing: %v", where, err)
			}
		case "<-":
			var got *jsonrpc.Message
			select {
			case got = <-incoming:
			case <-time.After(clientTimeout):
				t.Fatalf("%s: timed out waiting for %s", where, step.payload)
			}
			if got == nil {
				t.Fatalf("%s: connection closed while waiting for %s", where, step.payload)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var expected, actual any
			if err := json.Unmarshal(step.payload, &expected); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &actual); err != nil {
				t.Fatal(err)
			}
			if !subset(expected, actual) {
				t.Fatalf("%s: message mismatch\n got  %s\n want %s", where, raw, step.payload)
			}
			if got.IsRequest() {
				lastServerRequest = got.ID
			}
		case "=>":
			if lastServerRequest == nil {
				t.Fatalf("%s: reply without a preceding server request", where)
			}
			var result any
			if err := json.Unmarshal(step.payload, &result); err != nil {
				t.Fatal(err)
			}
			if err := client.Reply(lastServerRequest, result, nil); err != nil {
				t.Fatalf("%s: replying: %v", where, err)
			}
			lastServerRequest = nil
		}
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("%s: Run() error = %v", path, err)
		}
	case <-time.After(clientTimeout):
		t.Fatalf("%s: server did not exit at the end of the session", path)
	}
	_ = clientWriter.Close()
	for msg := range incoming {
		raw, _ := json.Marshal(msg)
		t.Errorf("%s: unexpected message after the session: %s", path, raw)
	}
}
