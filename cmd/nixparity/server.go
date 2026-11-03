package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/proctree"
)

const (
	shutdownGrace      = 5 * time.Second
	publishedMethod    = "textDocument/publishDiagnostics"
	configurationAsked = "workspace/configuration"
	utf16Encoding      = "utf-16"
)

type serverConfig struct {
	name    string
	argv    []string
	root    string
	timeout time.Duration
	init    json.RawMessage
	settle  time.Duration
}

type identity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type replyState uint8

const (
	replyResponded replyState = iota
	replyTimedOut
	replyDied
)

type reply struct {
	state   replyState
	result  json.RawMessage
	rpcErr  *jsonrpc.Error
	elapsed time.Duration
}

type publication struct {
	raw   json.RawMessage
	first time.Time
}

type server struct {
	cfg     serverConfig
	norm    normalizer
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	release func()
	stdin   *os.File
	stdout  *os.File
	conn    *jsonrpc.Conn
	sampler *rssSampler

	info         identity
	capabilities map[string]json.RawMessage
	nextID       int

	mu        sync.Mutex
	pending   map[string]chan *jsonrpc.Message
	published map[string]publication
	arrivals  chan struct{}

	dead     chan struct{}
	deadOnce sync.Once
	exited   chan struct{}
	readDone chan struct{}
}

func startServer(ctx context.Context, cfg serverConfig, norm normalizer) (*server, error) {
	childIn, stdin, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("creating the stdin pipe of %s: %w", cfg.name, err)
	}
	stdout, childOut, err := os.Pipe()
	if err != nil {
		_ = childIn.Close()
		_ = stdin.Close()
		return nil, fmt.Errorf("creating the stdout pipe of %s: %w", cfg.name, err)
	}
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, cfg.argv[0], cfg.argv[1:]...) //nolint:gosec
	cmd.Dir = cfg.root
	cmd.Stdin = childIn
	cmd.Stdout = childOut
	cmd.WaitDelay = proctree.WaitDelay
	release, err := proctree.Start(cmd)
	_ = childIn.Close()
	_ = childOut.Close()
	if err != nil {
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("starting %s (%s) in %s: %w", cfg.name, strings.Join(cfg.argv, " "), cfg.root, err)
	}
	s := &server{
		cfg:       cfg,
		norm:      norm,
		cmd:       cmd,
		cancel:    cancel,
		release:   release,
		stdin:     stdin,
		stdout:    stdout,
		conn:      jsonrpc.NewConn(stdout, stdin),
		sampler:   startRSSSampler(cmd.Process.Pid),
		pending:   map[string]chan *jsonrpc.Message{},
		published: map[string]publication{},
		arrivals:  make(chan struct{}, 1),
		dead:      make(chan struct{}),
		exited:    make(chan struct{}),
		readDone:  make(chan struct{}),
	}
	go s.readLoop()
	go s.waitLoop()
	if err := s.initialize(ctx); err != nil {
		_, closeErr := s.close()
		return nil, errors.Join(fmt.Errorf("initializing %s (%s) in %s: %w", cfg.name, strings.Join(cfg.argv, " "), cfg.root, err), closeErr)
	}
	return s, nil
}

func (s *server) markDead() {
	s.deadOnce.Do(func() { close(s.dead) })
}

func (s *server) isDead() bool {
	select {
	case <-s.dead:
		return true
	default:
		return false
	}
}

func (s *server) waitLoop() {
	defer close(s.exited)
	defer s.markDead()
	_ = s.cmd.Wait()
}

func (s *server) readLoop() {
	defer close(s.readDone)
	defer s.markDead()
	for {
		msg, err := s.conn.Read()
		if err != nil {
			return
		}
		switch {
		case msg.IsResponse():
			s.deliver(msg)
		case msg.IsRequest():
			go s.answerServerRequest(msg)
		case msg.Method == publishedMethod:
			s.recordPublication(msg.Params)
		}
	}
}

func (s *server) deliver(msg *jsonrpc.Message) {
	s.mu.Lock()
	ch, ok := s.pending[string(msg.ID)]
	delete(s.pending, string(msg.ID))
	s.mu.Unlock()
	if ok {
		ch <- msg
	}
}

func (s *server) answerServerRequest(msg *jsonrpc.Message) {
	var result any
	if msg.Method == configurationAsked {
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(msg.Params, &params); err == nil {
			result = make([]any, len(params.Items))
		}
	}
	_ = s.conn.Reply(msg.ID, result, nil)
}

func (s *server) recordPublication(params json.RawMessage) {
	var target struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &target); err != nil {
		return
	}
	key := s.norm.uri(target.URI)
	now := time.Now()
	s.mu.Lock()
	entry, seen := s.published[key]
	if !seen {
		entry.first = now
	}
	entry.raw = params
	s.published[key] = entry
	s.mu.Unlock()
	select {
	case s.arrivals <- struct{}{}:
	default:
	}
}

func (s *server) publication(rel string) (publication, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.published[rel]
	return entry, ok
}

func (s *server) publishedAll(rels []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rel := range rels {
		if _, ok := s.published[rel]; !ok {
			return false
		}
	}
	return true
}

func (s *server) awaitDiagnostics(ctx context.Context, rels []string, silence, settle time.Duration) {
	limit := time.NewTimer(s.cfg.timeout)
	defer limit.Stop()
	quiet := time.NewTimer(silence)
	defer quiet.Stop()
	for {
		select {
		case <-s.arrivals:
			if s.publishedAll(rels) {
				quiet.Reset(settle)
			} else {
				quiet.Reset(silence)
			}
		case <-quiet.C:
			return
		case <-limit.C:
			return
		case <-s.dead:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *server) initialize(ctx context.Context) error {
	uri := fileuri.FromPath(s.cfg.root)
	r := s.call(ctx, "initialize", initializeParams(uri, s.cfg.init), s.cfg.timeout)
	switch {
	case r.state == replyTimedOut:
		return fmt.Errorf("no answer within %s", s.cfg.timeout)
	case r.state == replyDied:
		return errors.New("the server exited")
	case r.rpcErr != nil:
		return r.rpcErr
	}
	var result struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
		ServerInfo   identity                   `json:"serverInfo"`
	}
	if err := json.Unmarshal(r.result, &result); err != nil {
		return fmt.Errorf("decoding the answer: %w", err)
	}
	var encoding string
	if raw, ok := result.Capabilities["positionEncoding"]; ok {
		if err := json.Unmarshal(raw, &encoding); err != nil {
			return fmt.Errorf("decoding the position encoding: %w", err)
		}
	}
	if encoding != "" && encoding != utf16Encoding {
		return fmt.Errorf("negotiated position encoding %q when only %s was offered", encoding, utf16Encoding)
	}
	s.info = result.ServerInfo
	if s.info.Name == "" {
		s.info.Name = s.cfg.name
	}
	s.capabilities = result.Capabilities
	return s.notify("initialized", map[string]any{})
}

func initializeParams(rootURI string, options json.RawMessage) map[string]any {
	params := map[string]any{
		"processId":        os.Getpid(),
		"rootUri":          rootURI,
		"workspaceFolders": []map[string]string{{"uri": rootURI, "name": "root"}},
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{utf16Encoding}},
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": true},
				"completion":         map[string]any{},
				"references":         map[string]any{},
				"rename":             map[string]any{},
				"foldingRange":       map[string]any{},
				"formatting":         map[string]any{},
			},
		},
	}
	if len(options) > 0 {
		params["initializationOptions"] = options
	}
	return params
}

func (s *server) supports(capability string) bool {
	raw, ok := s.capabilities[capability]
	if !ok || isNull(raw) {
		return false
	}
	return string(raw) != "false"
}

func (s *server) notify(method string, params any) error {
	written := make(chan error, 1)
	go func() { written <- s.conn.Notify(method, params) }()
	timer := time.NewTimer(s.cfg.timeout)
	defer timer.Stop()
	select {
	case err := <-written:
		if err != nil {
			return fmt.Errorf("sending %s to %s: %w", method, s.cfg.name, err)
		}
		return nil
	case <-s.dead:
		return fmt.Errorf("sending %s to %s: the server exited", method, s.cfg.name)
	case <-timer.C:
		return fmt.Errorf("sending %s to %s: not read within %s", method, s.cfg.name, s.cfg.timeout)
	}
}

func (s *server) call(ctx context.Context, method string, params any, timeout time.Duration) reply {
	s.nextID++
	id := json.RawMessage(strconv.Itoa(s.nextID))
	response := make(chan *jsonrpc.Message, 1)
	s.mu.Lock()
	s.pending[string(id)] = response
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, string(id))
		s.mu.Unlock()
	}()
	start := time.Now()
	written := make(chan error, 1)
	go func() { written <- s.conn.Request(id, method, params) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case err := <-written:
			if err != nil {
				return reply{state: replyDied}
			}
			written = nil
		case msg := <-response:
			return reply{state: replyResponded, result: msg.Result, rpcErr: msg.Error, elapsed: time.Since(start)}
		case <-s.dead:
			select {
			case msg := <-response:
				return reply{state: replyResponded, result: msg.Result, rpcErr: msg.Error, elapsed: time.Since(start)}
			default:
				return reply{state: replyDied}
			}
		case <-timer.C:
			go func() { _ = s.conn.Notify("$/cancelRequest", map[string]json.RawMessage{"id": id}) }()
			return reply{state: replyTimedOut}
		case <-ctx.Done():
			return reply{state: replyDied}
		}
	}
}

func (s *server) close() (*int64, error) {
	if !s.isDead() {
		s.call(context.Background(), "shutdown", nil, min(shutdownGrace, s.cfg.timeout))
		if err := s.notify("exit", nil); err != nil {
			fmt.Fprintf(os.Stderr, "nixparity: %v\n", err)
		}
	}
	_ = s.stdin.Close()
	timer := time.NewTimer(shutdownGrace)
	defer timer.Stop()
	select {
	case <-s.exited:
	case <-timer.C:
	}
	s.cancel()
	<-s.exited
	killErr := s.cmd.Cancel()
	_ = s.stdout.Close()
	<-s.readDone
	peak := s.sampler.finish(s.cfg.name)
	s.release()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return peak, fmt.Errorf("killing what is left of %s: %w", s.cfg.name, killErr)
	}
	return peak, nil
}
