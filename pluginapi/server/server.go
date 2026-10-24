package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi"
	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi/client"
)

type serverError string

func (e serverError) Error() string { return string(e) }

const (
	ErrMissingToken serverError = "server token is required"
)

const (
	defaultShutdownTimeout = 30 * time.Second
	defaultJobRetention    = 24 * time.Hour
	defaultRetryBackoff    = time.Second
	maxJanitorInterval     = time.Minute
	readHeaderTimeout      = 10 * time.Second
)

type Config struct {
	Addr            string
	Token           string
	MaxBodyBytes    int64
	ShutdownTimeout time.Duration
	JobRetention    time.Duration
	RetryBackoff    time.Duration
	HTTPClient      *http.Client
	Logger          *slog.Logger
	HealthCheck     func(ctx context.Context) pluginapi.Health
}

type action struct {
	impl   pluginapi.Action
	spec   pluginapi.ActionSpec
	input  *pluginapi.Schema
	output *pluginapi.Schema
}

type Server struct {
	cfg      Config
	manifest pluginapi.Manifest
	actions  map[string]*action
	core     *client.Core
	handler  http.Handler
	base     context.Context
	stop     context.CancelFunc
	wg       sync.WaitGroup

	mu     sync.Mutex
	jobs   map[string]*job
	closed bool
}

func New(cfg Config, info pluginapi.Info, actions ...pluginapi.Action) (*Server, error) {
	if cfg.Token == "" {
		return nil, ErrMissingToken
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = pluginapi.DefaultMaxBodyBytes
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.JobRetention == 0 {
		cfg.JobRetention = defaultJobRetention
	}
	if cfg.RetryBackoff == 0 {
		cfg.RetryBackoff = defaultRetryBackoff
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HealthCheck == nil {
		cfg.HealthCheck = func(context.Context) pluginapi.Health {
			return pluginapi.Health{Status: pluginapi.HealthOK}
		}
	}

	manifest := pluginapi.Manifest{
		Protocol:    pluginapi.ProtocolVersion,
		Name:        info.Name,
		Version:     info.Version,
		Description: info.Description,
		Triggers:    info.Triggers,
	}
	registry := make(map[string]*action, len(actions))
	for _, impl := range actions {
		spec := impl.Spec()
		manifest.Actions = append(manifest.Actions, spec)
		registry[spec.Name] = &action{impl: impl, spec: spec}
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("validate manifest: %w", err)
	}
	for _, act := range registry {
		input, err := pluginapi.CompileSchema(act.spec.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("compile input schema of %s: %w", act.spec.Name, err)
		}
		output, err := pluginapi.CompileSchema(act.spec.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("compile output schema of %s: %w", act.spec.Name, err)
		}
		act.input = input
		act.output = output
	}

	base, stop := context.WithCancel(context.Background())
	s := &Server{
		cfg:      cfg,
		manifest: manifest,
		actions:  registry,
		core:     client.NewCore("", info.Name, "", cfg.HTTPClient),
		base:     base,
		stop:     stop,
		jobs:     map[string]*job{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+pluginapi.PathManifest, s.handleManifest)
	mux.HandleFunc("GET "+pluginapi.PathHealth, s.handleHealth)
	mux.HandleFunc("POST "+pluginapi.PathStartJob, s.handleStartJob)
	mux.HandleFunc("GET "+pluginapi.PathJob, s.handleGetJob)
	mux.HandleFunc("POST "+pluginapi.PathCancelJob, s.handleCancelJob)
	mux.HandleFunc("GET "+pluginapi.PathJobEvents, s.handleEvents)
	mux.HandleFunc("POST "+pluginapi.PathJobMessages, s.handleMessages)
	s.handler = s.authenticate(mux)

	s.wg.Add(1)
	go s.janitor()
	return s, nil
}

func (s *Server) Manifest() pluginapi.Manifest {
	return s.manifest
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.stop()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	drained := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) janitor() {
	defer s.wg.Done()
	ticker := time.NewTicker(min(s.cfg.JobRetention/2, maxJanitorInterval))
	defer ticker.Stop()
	for {
		select {
		case <-s.base.Done():
			return
		case <-ticker.C:
		}
		cutoff := time.Now().Add(-s.cfg.JobRetention)
		s.mu.Lock()
		for id, j := range s.jobs {
			if j.expired(cutoff) {
				delete(s.jobs, id)
			}
		}
		s.mu.Unlock()
	}
}

func Serve(ctx context.Context, cfg Config, info pluginapi.Info, actions ...pluginapi.Action) error {
	s, err := New(cfg, info, actions...)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: cfg.Addr, Handler: s, ReadHeaderTimeout: readHeaderTimeout}
	listenDone := make(chan error, 1)
	go func() {
		listenDone <- httpServer.ListenAndServe()
	}()

	var listenErr error
	listenReturned := false
	select {
	case listenErr = <-listenDone:
		listenReturned = true
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()
	shutdownErr := errors.Join(s.Shutdown(shutdownCtx), httpServer.Shutdown(shutdownCtx))
	if !listenReturned {
		listenErr = <-listenDone
	}
	if errors.Is(listenErr, http.ErrServerClosed) {
		listenErr = nil
	}
	return errors.Join(listenErr, shutdownErr)
}
