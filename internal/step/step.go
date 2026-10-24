package step

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/go-playground/validator/v10"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
)

type stepError string

func (e stepError) Error() string { return string(e) }

const (
	ErrUnknownStep      stepError = "unknown step"
	ErrDuplicateStep    stepError = "step already registered"
	ErrInvalidParams    stepError = "invalid step params"
	ErrUndeclaredResult stepError = "step returned undeclared outcome"
)

const (
	QueueClaude  = "claude"
	QueueDefault = "default"
)

type Outcome string

const OutcomeOK Outcome = "ok"

type WaitRequest struct {
	Kind          process.WaitKind
	ResumeOutcome Outcome
}

type Result struct {
	Outcome Outcome
	Vars    process.Vars
	Wait    *WaitRequest
}

type Context struct {
	Process   process.Process
	StepRunID string
	WorkerID  string
	Handle    *runstore.RunHandle
}

type Step interface {
	Run(ctx context.Context, sc Context) (Result, error)
}

type NoParams struct{}

type Def struct {
	Name     string
	Queue    string
	Outcomes []Outcome
}

func (d Def) Declares(o Outcome) bool {
	return slices.Contains(d.Outcomes, o)
}

type entry struct {
	def   Def
	build func(raw json.RawMessage) (Step, error)
}

type Registry struct {
	mu       sync.RWMutex
	entries  map[string]entry
	validate *validator.Validate
}

func NewRegistry() *Registry {
	return &Registry{
		entries:  map[string]entry{},
		validate: validator.New(validator.WithRequiredStructEnabled()),
	}
}

func Register[P any](r *Registry, def Def, build func(P) Step) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[def.Name]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateStep, def.Name)
	}
	r.entries[def.Name] = entry{
		def: def,
		build: func(raw json.RawMessage) (Step, error) {
			params, err := decodeParams[P](r.validate, raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", def.Name, err)
			}
			return build(params), nil
		},
	}
	return nil
}

func (r *Registry) Lookup(name string) (Def, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	return e.def, ok
}

func (r *Registry) Build(name string, params json.RawMessage) (Step, error) {
	r.mu.RLock()
	e, ok := r.entries[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownStep, name)
	}
	return e.build(params)
}

func decodeParams[P any](v *validator.Validate, raw json.RawMessage) (P, error) {
	var params P
	if len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return params, fmt.Errorf("%w: %w", ErrInvalidParams, err)
		}
	}
	if err := v.Struct(params); err != nil {
		return params, fmt.Errorf("%w: %w", ErrInvalidParams, err)
	}
	return params, nil
}

const ErrUpstream stepError = "upstream dependency failed"
