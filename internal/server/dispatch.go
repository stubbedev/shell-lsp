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
	"time"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	exitMethod     = "exit"
	maxConcurrency = 8
)

type requestHandler func(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error)

type notificationHandler func(ctx context.Context, params json.RawMessage) error

type invalidParams struct {
	err error
}

func (e *invalidParams) Error() string { return e.err.Error() }

func (e *invalidParams) Unwrap() error { return e.err }

type requestBinding func(*Server) requestHandler

func (b requestBinding) install(s *Server, name string) { s.requests[name] = b(s) }

type notificationBinding func(*Server) notificationHandler

func (b notificationBinding) install(s *Server, name string) { s.notifications[name] = b(s) }

func onRequest[P any](handle func(*Server, context.Context, *docstore.View, P) (any, error)) requestBinding {
	return func(s *Server) requestHandler {
		return func(ctx context.Context, docs *docstore.View, raw json.RawMessage) (any, error) {
			params, err := lsp.DecodeParams[P](raw)
			if err != nil {
				return nil, &invalidParams{err}
			}
			return handle(s, ctx, docs, params)
		}
	}
}

func onNotification[P any](handle func(*Server, context.Context, P) error) notificationBinding {
	return func(s *Server) notificationHandler {
		return func(ctx context.Context, raw json.RawMessage) error {
			params, err := lsp.DecodeParams[P](raw)
			if err != nil {
				return &invalidParams{err}
			}
			return handle(s, ctx, params)
		}
	}
}

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
	warming     *coalescer
	outbound    *coalescer
	push        *pushScheduler
	commandDocs commandDocs
	prefetch    *prefetcher
	environment shellEnvironment
	workspace   *workspace.Workspace
	semantic    *semanticCache
	bats        *batsResults

	mu             sync.Mutex
	shutdown       bool
	initialized    bool
	trace          TraceValue
	settings       config.Settings
	folderSettings map[string]config.Settings
	generation     uint64
	client         ClientCapabilities
	capabilities   ServerCapabilities

	requests      map[string]requestHandler
	notifications map[string]notificationHandler

	pending  sync.Map
	nextID   atomic.Int64
	cancels  sync.Map
	progress sync.Map
	notices  sync.Map

	process      Process
	editorconfig *editorconfig.Resolver
	shellcheckrc *shellcheckrc.Resolver
	sem          chan struct{}
	exiting      chan struct{}
	wg           sync.WaitGroup
}

func New(conn *jsonrpc.Conn, version string, process Process) *Server {
	docs := docstore.NewStore()
	s := &Server{
		conn:         conn,
		version:      version,
		process:      process,
		logger:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
		docs:         docs,
		snapshots:    newSnapshotCache(docs.Has),
		warming:      newCoalescer(),
		outbound:     newCoalescer(),
		semantic:     newSemanticCache(docs.Has),
		bats:         newBatsResults(),
		settings:     config.Default(),
		editorconfig: editorconfig.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck),
		shellcheckrc: newShellcheckrcResolver(shellcheckrc.Fallback(process.Home, process.ConfigHome)),
		sem:          make(chan struct{}, maxConcurrency),
		exiting:      make(chan struct{}),
	}
	s.usePages(manpage.NewLookup(process.ManPath, process.Path))
	s.workspace = newWorkspace(s)
	s.registerHandlers()
	return s
}

func (s *Server) usePages(pages *manpage.Lookup) {
	s.prefetch = newPrefetcher(pages)
	s.commandDocs = commandDocs{Lookup: pages, path: s.process.pathDirs(), pathExt: s.process.pathExt()}
	s.environment = shellEnvironment{process: s.process, pages: pages, executables: newExecutableCache(s.process.pathDirs(), s.process.pathExt()), prefetch: s.prefetch}
}

func (s *Server) SetLogger(logger *slog.Logger) {
	s.logger = logger
}

func (s *Server) registerHandlers() {
	s.requests = map[string]requestHandler{}
	s.notifications = map[string]notificationHandler{}
	for _, m := range methods {
		if m.handler != nil {
			m.handler.install(s, m.name)
		}
		if m.capability.advertise != nil {
			m.capability.advertise(&s.capabilities)
		}
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
			s.traceIncoming("notification", msg.Method, nil, msg.Params)
			if msg.Method == exitMethod {
				close(s.exiting)
				s.wg.Wait()
				if s.isShutdown() {
					return nil
				}
				return errors.New("exit before shutdown")
			}
			s.dispatchNotification(rootCtx, msg)
		case msg.IsRequest():
			s.traceIncoming("request", msg.Method, msg.ID, msg.Params)
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
	initialized := s.isInitialized()
	if msg.Method == "initialize" && initialized {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeInvalidRequest, "the server is already initialized"))
		return
	}
	if msg.Method == "shutdown" && !initialized {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeServerNotInitialized, "shutdown before initialize"))
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
	if !s.isInitialized() {
		s.replyError(msg, jsonrpc.NewError(jsonrpc.CodeServerNotInitialized,
			"request %q before initialize", msg.Method))
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
		rctx := context.WithValue(rctx, requestSlotKey{}, s.sem)

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
	result, err = handler(ctx, docs, msg.Params)
	if invalid, ok := errors.AsType[*invalidParams](err); ok {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing %s params: %v", msg.Method, invalid.err)
	}
	return result, err
}

func (s *Server) dispatchNotification(ctx context.Context, msg *jsonrpc.Message) {
	if !s.isInitialized() {
		s.logger.Warn("dropping notification before initialize", "method", msg.Method)
		return
	}
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
	case <-s.exiting:
		return fmt.Errorf("%s: the client sent exit before answering", method)
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
