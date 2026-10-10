package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	exitMethod     = "exit"
	maxConcurrency = 8
)

type requestHandler func(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error)

type notificationHandler func(ctx context.Context, params json.RawMessage) error

type TraceValue string

const (
	TraceOff      TraceValue = "off"
	TraceMessages TraceValue = "messages"
	TraceVerbose  TraceValue = "verbose"
)

type Server struct {
	conn        *jsonrpc.Conn
	version     string
	logger      *slog.Logger
	docs        *docstore.Store
	snapshots   *snapshotCache
	diagnostics *diagnosticsState
	push        *pushScheduler
	commandDocs commandDocs
	environment shellEnvironment
	workspace   *workspace.Workspace

	mu          sync.Mutex
	shutdown    bool
	initialized bool
	trace       TraceValue
	settings    config.Settings
	generation  uint64
	client      ClientCapabilities

	requests      map[string]requestHandler
	notifications map[string]notificationHandler

	pending sync.Map
	nextID  atomic.Int64
	cancels sync.Map
	notices sync.Map
	sem     chan struct{}
	wg      sync.WaitGroup
}

func New(conn *jsonrpc.Conn, version string) *Server {
	pages := manpage.NewLookup()
	s := &Server{
		conn:        conn,
		version:     version,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
		docs:        docstore.NewStore(),
		snapshots:   newSnapshotCache(),
		diagnostics: newDiagnosticsState(),
		settings:    config.Default(),
		commandDocs: commandDocs{pages: pages},
		environment: shellEnvironment{pages: pages, executables: &executableCache{}},
		sem:         make(chan struct{}, maxConcurrency),
	}
	s.workspace = newWorkspace(s)
	s.registerHandlers()
	return s
}

func (s *Server) SetLogger(logger *slog.Logger) {
	s.logger = logger
}

func (s *Server) registerHandlers() {
	s.requests = map[string]requestHandler{
		"initialize":                  s.handleInitialize,
		"shutdown":                    s.handleShutdown,
		"textDocument/diagnostic":     s.handleDocumentDiagnostic,
		"textDocument/hover":          s.handleHover,
		"textDocument/completion":     s.handleCompletion,
		"completionItem/resolve":      s.handleCompletionResolve,
		"textDocument/definition":     s.navigationHandler(navigation.ModeDefinition),
		"textDocument/declaration":    s.navigationHandler(navigation.ModeDeclaration),
		"textDocument/typeDefinition": s.navigationHandler(navigation.ModeTypeDefinition),
		"textDocument/implementation": s.navigationHandler(navigation.ModeImplementation),
	}
	s.notifications = map[string]notificationHandler{
		"$/cancelRequest":                  s.handleCancelRequest,
		"$/setTrace":                       s.handleSetTrace,
		"initialized":                      s.handleInitialized,
		"window/logMessage":                s.handleClientLogMessage,
		"textDocument/didOpen":             s.handleDidOpen,
		"textDocument/didChange":           s.handleDidChange,
		"textDocument/didClose":            s.handleDidClose,
		"textDocument/didSave":             s.handleDidSave,
		"workspace/didChangeConfiguration": s.handleDidChangeConfiguration,
		"workspace/didChangeWatchedFiles":  s.handleDidChangeWatchedFiles,
	}
}

func (s *Server) Run(ctx context.Context) error {
	rootCtx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		s.wg.Wait()
		if s.push != nil {
			s.push.close()
		}
	}()
	for {
		msg, err := s.conn.Read()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				if s.isShutdown() {
					return nil
				}
				return errors.New("client closed input without exit")
			}
			return fmt.Errorf("reading message: %w", err)
		}
		switch {
		case msg.IsResponse():
			s.routeResponse(msg)
		case msg.IsNotification():
			if msg.Method == exitMethod {
				s.wg.Wait()
				if s.isShutdown() {
					return nil
				}
				return errors.New("exit before shutdown")
			}
			s.dispatchNotification(rootCtx, msg)
		case msg.IsRequest():
			if msg.Method == "initialize" || msg.Method == "shutdown" {
				s.handleLifecycleRequest(msg)
				continue
			}
			s.dispatchRequest(rootCtx, msg)
		}
	}
}

func (s *Server) handleLifecycleRequest(msg *jsonrpc.Message) {
	if s.isShutdown() {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeInvalidRequest,
			"request %q after shutdown", msg.Method))
		return
	}
	handler, ok := s.requests[msg.Method]
	if !ok {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeMethodNotFound,
			"method %q not found", msg.Method))
		return
	}
	result, err := s.invoke(context.Background(), handler, s.docs.View(), msg)
	if err != nil {
		s.replyError(msg, err)
		return
	}
	if err := s.conn.Reply(msg.ID, result, nil); err != nil {
		s.logger.Error("writing response", "method", msg.Method, "err", err)
	}
}

func (s *Server) dispatchRequest(ctx context.Context, msg *jsonrpc.Message) {
	if s.isShutdown() {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeInvalidRequest,
			"request %q after shutdown", msg.Method))
		return
	}
	if !s.isInitialized() && msg.Method != "initialize" {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeInvalidRequest,
			"request %q before initialize", msg.Method))
		return
	}
	if s.isShutdown() {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeInvalidRequest,
			"request %q after shutdown", msg.Method))
		return
	}
	handler, ok := s.requests[msg.Method]
	if !ok {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeMethodNotFound,
			"method %q not found", msg.Method))
		return
	}
	docs := s.docs.View()
	s.wg.Add(1)
	rctx, cancel := context.WithCancel(ctx)
	s.cancels.Store(string(msg.ID), cancel)
	go func() {
		defer s.wg.Done()
		select {
		case s.sem <- struct{}{}:
		case <-rctx.Done():
			cancel()
			s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeRequestCancelled,
				"request %q was cancelled before processing", msg.Method))
			return
		}
		defer func() { <-s.sem }()
		defer cancel()
		defer s.cancels.Delete(string(msg.ID))

		result, err := s.invoke(rctx, handler, docs, msg)
		if errors.Is(err, context.Canceled) && rctx.Err() != nil {
			err = jsonrpc.NewError(jsonrpc.CodeRequestCancelled,
				"request %q was cancelled", msg.Method)
		}
		if err != nil {
			s.replyError(msg, err)
			return
		}
		if err := s.conn.Reply(msg.ID, result, nil); err != nil {
			s.logger.Error("writing response", "method", msg.Method, "err", err)
		}
	}()
}

func (s *Server) invoke(ctx context.Context, handler requestHandler, docs *docstore.View, msg *jsonrpc.Message) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = jsonrpc.NewError(jsonrpc.CodeInternalError,
				"panic handling %s: %v", msg.Method, r)
		}
	}()
	return handler(ctx, docs, msg.Params)
}

func (s *Server) dispatchNotification(ctx context.Context, msg *jsonrpc.Message) {
	handler, ok := s.notifications[msg.Method]
	if !ok {
		if strings.HasPrefix(msg.Method, "$/") {
			return
		}
		s.logger.Warn("ignoring unknown notification", "method", msg.Method)
		return
	}
	if err := handler(ctx, msg.Params); err != nil {
		s.logger.Error("handling notification", "method", msg.Method, "err", err)
	}
}

func (s *Server) replyError(msg *jsonrpc.Message, err error) {
	rpcErr, isRPC := errors.AsType[*jsonrpc.Error](err)
	if isRPC {
		if writeErr := s.conn.Reply(msg.ID, nil, rpcErr); writeErr != nil {
			s.logger.Error("writing error response", "method", msg.Method, "err", writeErr)
		}
		return
	}
	if writeErr := s.conn.Reply(msg.ID, nil, jsonrpc.NewError(jsonrpc.CodeInternalError, "%v", err)); writeErr != nil {
		s.logger.Error("writing error response", "method", msg.Method, "err", writeErr)
	}
}

func (s *Server) routeResponse(msg *jsonrpc.Message) {
	waiter, ok := s.pending.LoadAndDelete(string(msg.ID))
	if !ok {
		s.logger.Warn("response with unknown request id", "id", string(msg.ID))
		return
	}
	ch, ok := waiter.(chan *jsonrpc.Message)
	if !ok {
		s.logger.Error("unexpected pending call entry", "id", string(msg.ID))
		return
	}
	select {
	case ch <- msg:
	default:
		s.logger.Warn("dropping response for expired call", "id", string(msg.ID))
	}
}

func (s *Server) call(ctx context.Context, method string, params, result any) error {
	id := json.RawMessage(strconv.FormatInt(s.nextID.Add(1), 10))
	waiter := make(chan *jsonrpc.Message, 1)
	s.pending.Store(string(id), waiter)
	defer s.pending.Delete(string(id))
	if err := s.conn.Request(id, method, params); err != nil {
		return fmt.Errorf("sending %s request: %w", method, err)
	}
	select {
	case msg := <-waiter:
		if msg.Error != nil {
			return fmt.Errorf("%s failed: %w", method, msg.Error)
		}
		if result == nil || len(msg.Result) == 0 || string(msg.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(msg.Result, result); err != nil {
			return fmt.Errorf("parsing %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) isInitialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

func (s *Server) isShutdown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdown
}
