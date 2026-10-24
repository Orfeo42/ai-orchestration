package pluginapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

type Mode string

const (
	ModeSync  Mode = "sync"
	ModeAsync Mode = "async"
)

type Capability string

const (
	CapabilityEvents Capability = "events"
	CapabilityInject Capability = "inject"
)

type Outcome string

const OutcomeOK Outcome = "ok"

type Info struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Triggers    []TriggerSpec `json:"triggers,omitempty"`
}

type Manifest struct {
	Protocol    string        `json:"protocol"`
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Actions     []ActionSpec  `json:"actions"`
	Triggers    []TriggerSpec `json:"triggers,omitempty"`
}

type ActionSpec struct {
	Name                  string          `json:"name"`
	Description           string          `json:"description"`
	Mode                  Mode            `json:"mode"`
	InputSchema           json.RawMessage `json:"input_schema"`
	OutputSchema          json.RawMessage `json:"output_schema"`
	Outcomes              []Outcome       `json:"outcomes"`
	Capabilities          []Capability    `json:"capabilities,omitempty"`
	DefaultTimeoutSeconds int             `json:"default_timeout_seconds"`
}

type TriggerSpec struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	PayloadSchema json.RawMessage `json:"payload_schema"`
}

var (
	pluginNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	actionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)
	outcomePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

func (m Manifest) Validate() error {
	var problems []error
	if m.Protocol != ProtocolVersion {
		problems = append(problems, fmt.Errorf("%w: got %q, want %q", ErrUnsupportedProtocol, m.Protocol, ProtocolVersion))
	}
	if !pluginNamePattern.MatchString(m.Name) {
		problems = append(problems, fmt.Errorf("%w: name %q must match %s", ErrInvalidManifest, m.Name, pluginNamePattern))
	}
	if m.Version == "" {
		problems = append(problems, fmt.Errorf("%w: version is empty", ErrInvalidManifest))
	}
	if m.Description == "" {
		problems = append(problems, fmt.Errorf("%w: description is empty", ErrInvalidManifest))
	}
	if len(m.Actions) == 0 {
		problems = append(problems, fmt.Errorf("%w: no actions declared", ErrInvalidManifest))
	}
	seenActions := map[string]bool{}
	for _, a := range m.Actions {
		if seenActions[a.Name] {
			problems = append(problems, fmt.Errorf("%w: duplicate action %q", ErrInvalidManifest, a.Name))
		}
		seenActions[a.Name] = true
		problems = append(problems, a.problems()...)
	}
	seenTriggers := map[string]bool{}
	for _, t := range m.Triggers {
		if seenTriggers[t.Name] {
			problems = append(problems, fmt.Errorf("%w: duplicate trigger %q", ErrInvalidManifest, t.Name))
		}
		seenTriggers[t.Name] = true
		problems = append(problems, t.problems()...)
	}
	return errors.Join(problems...)
}

func (a ActionSpec) problems() []error {
	var problems []error
	if !actionNamePattern.MatchString(a.Name) {
		problems = append(problems, fmt.Errorf("%w: action name %q must match %s", ErrInvalidManifest, a.Name, actionNamePattern))
	}
	if a.Description == "" {
		problems = append(problems, fmt.Errorf("%w: action %q description is empty", ErrInvalidManifest, a.Name))
	}
	if a.Mode != ModeSync && a.Mode != ModeAsync {
		problems = append(problems, fmt.Errorf("%w: action %q mode %q must be %q or %q", ErrInvalidManifest, a.Name, a.Mode, ModeSync, ModeAsync))
	}
	if len(a.Outcomes) == 0 {
		problems = append(problems, fmt.Errorf("%w: action %q declares no outcomes", ErrInvalidManifest, a.Name))
	}
	seenOutcomes := map[Outcome]bool{}
	for _, o := range a.Outcomes {
		if !outcomePattern.MatchString(string(o)) {
			problems = append(problems, fmt.Errorf("%w: action %q outcome %q must match %s", ErrInvalidManifest, a.Name, o, outcomePattern))
		}
		if seenOutcomes[o] {
			problems = append(problems, fmt.Errorf("%w: action %q duplicate outcome %q", ErrInvalidManifest, a.Name, o))
		}
		seenOutcomes[o] = true
	}
	seenCapabilities := map[Capability]bool{}
	for _, c := range a.Capabilities {
		if c != CapabilityEvents && c != CapabilityInject {
			problems = append(problems, fmt.Errorf("%w: action %q unknown capability %q", ErrInvalidManifest, a.Name, c))
		}
		if seenCapabilities[c] {
			problems = append(problems, fmt.Errorf("%w: action %q duplicate capability %q", ErrInvalidManifest, a.Name, c))
		}
		seenCapabilities[c] = true
	}
	if a.DefaultTimeoutSeconds < 1 {
		problems = append(problems, fmt.Errorf("%w: action %q default_timeout_seconds must be >= 1", ErrInvalidManifest, a.Name))
	}
	if _, err := CompileSchema(a.InputSchema); err != nil {
		problems = append(problems, fmt.Errorf("%w: action %q input_schema: %w", ErrInvalidManifest, a.Name, err))
	}
	if _, err := CompileSchema(a.OutputSchema); err != nil {
		problems = append(problems, fmt.Errorf("%w: action %q output_schema: %w", ErrInvalidManifest, a.Name, err))
	}
	return problems
}

func (t TriggerSpec) problems() []error {
	var problems []error
	if !actionNamePattern.MatchString(t.Name) {
		problems = append(problems, fmt.Errorf("%w: trigger name %q must match %s", ErrInvalidManifest, t.Name, actionNamePattern))
	}
	if t.Description == "" {
		problems = append(problems, fmt.Errorf("%w: trigger %q description is empty", ErrInvalidManifest, t.Name))
	}
	if _, err := CompileSchema(t.PayloadSchema); err != nil {
		problems = append(problems, fmt.Errorf("%w: trigger %q payload_schema: %w", ErrInvalidManifest, t.Name, err))
	}
	return problems
}

func (m Manifest) Action(name string) (ActionSpec, bool) {
	for _, a := range m.Actions {
		if a.Name == name {
			return a, true
		}
	}
	return ActionSpec{}, false
}

func (a ActionSpec) Declares(o Outcome) bool {
	return slices.Contains(a.Outcomes, o)
}

func (a ActionSpec) Has(c Capability) bool {
	return slices.Contains(a.Capabilities, c)
}
