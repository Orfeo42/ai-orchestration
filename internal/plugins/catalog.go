package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

var _ step.Catalog = (*Catalog)(nil)

type TriggerDef struct {
	Plugin        string          `json:"plugin"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	PayloadSchema json.RawMessage `json:"payload_schema"`
}

type Catalog struct {
	static  *step.Registry
	store   Store
	clients *Clients
	signer  *Signer

	mu      sync.RWMutex
	plugins map[string]Record
}

func NewCatalog(static *step.Registry, store Store, clients *Clients, signer *Signer) *Catalog {
	return &Catalog{static: static, store: store, clients: clients, signer: signer, plugins: map[string]Record{}}
}

func (c *Catalog) Refresh(ctx context.Context) error {
	records, err := c.store.ListPlugins(ctx)
	if err != nil {
		return fmt.Errorf("list plugins: %w", err)
	}
	c.set(records)
	return nil
}

func (c *Catalog) set(records []Record) {
	next := make(map[string]Record, len(records))
	for _, r := range records {
		next[r.Name] = r
	}
	c.mu.Lock()
	c.plugins = next
	c.mu.Unlock()
}

func (c *Catalog) Plugin(name string) (Record, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.plugins[name]
	return r, ok
}

func (c *Catalog) Action(uses string) (Record, pluginapi.ActionSpec, bool) {
	plugin, action, ok := SplitUses(uses)
	if !ok {
		return Record{}, pluginapi.ActionSpec{}, false
	}
	rec, ok := c.Plugin(plugin)
	if !ok {
		return Record{}, pluginapi.ActionSpec{}, false
	}
	spec, ok := rec.Manifest.Action(action)
	if !ok {
		return Record{}, pluginapi.ActionSpec{}, false
	}
	return rec, spec, true
}

func actionDef(plugin string, spec pluginapi.ActionSpec) step.Def {
	outcomes := make([]step.Outcome, len(spec.Outcomes))
	for i, o := range spec.Outcomes {
		outcomes[i] = step.Outcome(o)
	}
	return step.Def{
		Name:        plugin + "/" + spec.Name,
		Description: spec.Description,
		Queue:       StepQueue,
		Outcomes:    outcomes,
		InputSchema: spec.InputSchema,
		Plugin:      plugin,
	}
}

func (c *Catalog) Lookup(name string) (step.Def, bool) {
	if _, _, ok := SplitUses(name); !ok {
		return c.static.Lookup(name)
	}
	rec, spec, ok := c.Action(name)
	if !ok {
		return step.Def{}, false
	}
	return actionDef(rec.Name, spec), true
}

func (c *Catalog) Build(name string, params json.RawMessage) (step.Step, error) {
	if _, _, ok := SplitUses(name); !ok {
		return c.static.Build(name, params)
	}
	rec, spec, ok := c.Action(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", step.ErrUnknownStep, name)
	}
	return newStep(c, rec, spec, params)
}

func (c *Catalog) Defs() []step.Def {
	defs := c.static.Defs()
	c.mu.RLock()
	for _, rec := range c.plugins {
		for _, spec := range rec.Manifest.Actions {
			defs = append(defs, actionDef(rec.Name, spec))
		}
	}
	c.mu.RUnlock()
	slices.SortFunc(defs, func(a, b step.Def) int { return strings.Compare(a.Name, b.Name) })
	return defs
}

func (c *Catalog) trigger(plugin, name string) (pluginapi.TriggerSpec, bool) {
	rec, ok := c.Plugin(plugin)
	if !ok {
		return pluginapi.TriggerSpec{}, false
	}
	i := slices.IndexFunc(rec.Manifest.Triggers, func(t pluginapi.TriggerSpec) bool { return t.Name == name })
	if i < 0 {
		return pluginapi.TriggerSpec{}, false
	}
	return rec.Manifest.Triggers[i], true
}

func (c *Catalog) HasTrigger(plugin, trigger string) bool {
	_, ok := c.trigger(plugin, trigger)
	return ok
}

func (c *Catalog) Triggers() []TriggerDef {
	var defs []TriggerDef
	c.mu.RLock()
	for _, rec := range c.plugins {
		for _, t := range rec.Manifest.Triggers {
			defs = append(defs, TriggerDef{Plugin: rec.Name, Name: t.Name, Description: t.Description, PayloadSchema: t.PayloadSchema})
		}
	}
	c.mu.RUnlock()
	slices.SortFunc(defs, func(a, b TriggerDef) int {
		return strings.Compare(a.Plugin+"/"+a.Name, b.Plugin+"/"+b.Name)
	})
	return defs
}

func (c *Catalog) ValidateTrigger(ev pluginapi.TriggerEvent) error {
	if _, ok := c.Plugin(ev.Plugin); !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, ev.Plugin)
	}
	spec, ok := c.trigger(ev.Plugin, ev.Trigger)
	if !ok {
		return fmt.Errorf("%w: %s/%s", ErrUnknownTrigger, ev.Plugin, ev.Trigger)
	}
	schema, err := pluginapi.CompileSchema(spec.PayloadSchema)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if err := schema.Validate(payload); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	return nil
}
