package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type workflowError string

func (e workflowError) Error() string { return string(e) }

const (
	ErrInvalid         workflowError = "invalid workflow"
	ErrUnknownStep     workflowError = "unknown workflow step"
	ErrUnhandledResult workflowError = "outcome has no transition"
)

const (
	TargetEnd  = "end"
	TargetFail = "fail"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

type ForgejoTrigger struct {
	Repos    []string `json:"repos"`
	Label    string   `json:"label"`
	Assignee string   `json:"assignee,omitempty"`
}

type Trigger struct {
	Forgejo *ForgejoTrigger            `json:"forgejo,omitempty"`
	Plugin  string                     `json:"plugin,omitempty"`
	Event   string                     `json:"event,omitempty"`
	Match   map[string]json.RawMessage `json:"match,omitempty"`
}

type StepDef struct {
	Description string                  `json:"description,omitempty"`
	Uses        string                  `json:"uses"`
	With        json.RawMessage         `json:"with,omitempty"`
	Timeout     Duration                `json:"timeout,omitempty"`
	Retry       int                     `json:"retry,omitempty"`
	On          map[step.Outcome]string `json:"on"`
}

type Definition struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Trigger     Trigger            `json:"trigger"`
	Start       string             `json:"start"`
	Steps       map[string]StepDef `json:"steps"`
}

func Hash(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

func (d Definition) Step(id string) (StepDef, error) {
	s, ok := d.Steps[id]
	if !ok {
		return StepDef{}, fmt.Errorf("%w: %s", ErrUnknownStep, id)
	}
	return s, nil
}

func (d Definition) Next(stepID string, outcome step.Outcome) (string, error) {
	s, err := d.Step(stepID)
	if err != nil {
		return "", err
	}
	target, ok := s.On[outcome]
	if !ok {
		return "", fmt.Errorf("%w: step %s outcome %s", ErrUnhandledResult, stepID, outcome)
	}
	return target, nil
}

func Parse(source []byte) (Definition, error) {
	raw, err := yaml.YAMLToJSON(source)
	if err != nil {
		return Definition{}, fmt.Errorf("%w: yaml: %w", ErrInvalid, err)
	}
	return ParseJSON(raw)
}

func (d Definition) Validate(reg step.Catalog) error {
	var errs []error
	if d.Name == "" {
		errs = append(errs, fmt.Errorf("%w: name is required", ErrInvalid))
	}
	errs = append(errs, d.Trigger.validate(reg)...)
	if len(d.Steps) == 0 {
		errs = append(errs, fmt.Errorf("%w: at least one step is required", ErrInvalid))
	}
	if _, ok := d.Steps[d.Start]; !ok {
		errs = append(errs, fmt.Errorf("%w: start %q is not a defined step", ErrInvalid, d.Start))
	}
	for _, id := range slices.Sorted(maps.Keys(d.Steps)) {
		errs = append(errs, d.validateStep(reg, id)...)
	}
	errs = append(errs, d.validateReachable()...)
	return errors.Join(errs...)
}

func (t Trigger) validate(cat step.Catalog) []error {
	var errs []error
	if t.Forgejo != nil && t.Plugin != "" {
		errs = append(errs, fmt.Errorf("%w: trigger.forgejo and trigger.plugin are mutually exclusive", ErrInvalid))
	}
	if t.Forgejo != nil {
		errs = append(errs, t.Forgejo.validate()...)
	}
	if t.Plugin == "" && t.Event != "" {
		errs = append(errs, fmt.Errorf("%w: trigger.event requires trigger.plugin", ErrInvalid))
	}
	if t.Plugin == "" && len(t.Match) > 0 {
		errs = append(errs, fmt.Errorf("%w: trigger.match requires trigger.plugin", ErrInvalid))
	}
	if t.Plugin != "" && t.Event == "" {
		errs = append(errs, fmt.Errorf("%w: trigger.event is required with trigger.plugin", ErrInvalid))
	}
	if t.Plugin != "" && t.Event != "" && !cat.HasTrigger(t.Plugin, t.Event) {
		errs = append(errs, fmt.Errorf("%w: trigger %s/%s is not provided by any plugin", ErrInvalid, t.Plugin, t.Event))
	}
	for _, key := range slices.Sorted(maps.Keys(t.Match)) {
		if key == "" {
			errs = append(errs, fmt.Errorf("%w: trigger.match keys must not be empty", ErrInvalid))
		}
	}
	return errs
}

func (f ForgejoTrigger) validate() []error {
	var errs []error
	if len(f.Repos) == 0 {
		errs = append(errs, fmt.Errorf("%w: trigger.forgejo.repos needs at least one repo", ErrInvalid))
	}
	for _, repo := range f.Repos {
		owner, name, found := strings.Cut(repo, "/")
		if !found || owner == "" || name == "" || strings.Contains(name, "/") {
			errs = append(errs, fmt.Errorf("%w: trigger.forgejo.repos entry %q must be owner/repo", ErrInvalid, repo))
		}
	}
	if f.Label == "" {
		errs = append(errs, fmt.Errorf("%w: trigger.forgejo.label is required", ErrInvalid))
	}
	return errs
}

func (d Definition) validateStep(reg step.Catalog, id string) []error {
	s := d.Steps[id]
	var errs []error
	if id == TargetEnd || id == TargetFail {
		errs = append(errs, fmt.Errorf("%w: step id %q is reserved", ErrInvalid, id))
	}
	if s.Retry < 0 {
		errs = append(errs, fmt.Errorf("%w: step %s: retry must be >= 0", ErrInvalid, id))
	}
	if s.Timeout < 0 {
		errs = append(errs, fmt.Errorf("%w: step %s: timeout must be >= 0", ErrInvalid, id))
	}
	for _, outcome := range slices.Sorted(maps.Keys(s.On)) {
		target := s.On[outcome]
		if _, ok := d.Steps[target]; !ok && target != TargetEnd && target != TargetFail {
			errs = append(errs, fmt.Errorf("%w: step %s: outcome %s targets unknown step %q", ErrInvalid, id, outcome, target))
		}
	}
	def, ok := reg.Lookup(s.Uses)
	if !ok {
		return append(errs, fmt.Errorf("%w: step %s: uses %q is not registered", ErrInvalid, id, s.Uses))
	}
	if _, err := reg.Build(s.Uses, s.With); err != nil {
		errs = append(errs, fmt.Errorf("%w: step %s: with: %w", ErrInvalid, id, err))
	}
	for _, outcome := range def.Outcomes {
		if _, ok := s.On[outcome]; !ok {
			errs = append(errs, fmt.Errorf("%w: step %s: outcome %s has no on entry", ErrInvalid, id, outcome))
		}
	}
	for _, outcome := range slices.Sorted(maps.Keys(s.On)) {
		if !def.Declares(outcome) {
			errs = append(errs, fmt.Errorf("%w: step %s: on key %s is not an outcome of %s", ErrInvalid, id, outcome, s.Uses))
		}
	}
	return errs
}

func (d Definition) validateReachable() []error {
	if _, ok := d.Steps[d.Start]; !ok {
		return nil
	}
	seen := map[string]bool{d.Start: true}
	queue := []string{d.Start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, target := range d.Steps[current].On {
			if _, ok := d.Steps[target]; !ok || seen[target] {
				continue
			}
			seen[target] = true
			queue = append(queue, target)
		}
	}
	var errs []error
	for _, id := range slices.Sorted(maps.Keys(d.Steps)) {
		if !seen[id] {
			errs = append(errs, fmt.Errorf("%w: step %s is unreachable from start", ErrInvalid, id))
		}
	}
	return errs
}

func ParseJSON(raw []byte) (Definition, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return Definition{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	dec := json.NewDecoder(&compact)
	dec.DisallowUnknownFields()
	var d Definition
	if err := dec.Decode(&d); err != nil {
		return Definition{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return d, nil
}

func (d Definition) YAML() ([]byte, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("encode workflow: %w", err)
	}
	out, err := yaml.JSONToYAML(raw)
	if err != nil {
		return nil, fmt.Errorf("convert workflow to yaml: %w", err)
	}
	return out, nil
}
