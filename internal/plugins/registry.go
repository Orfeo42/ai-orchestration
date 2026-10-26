package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type WorkflowSource interface {
	LatestWorkflowVersions(ctx context.Context) ([]process.WorkflowVersion, error)
}

type Registry struct {
	store     Store
	clients   *Clients
	catalog   *Catalog
	workflows WorkflowSource
}

func NewRegistry(store Store, clients *Clients, catalog *Catalog, workflows WorkflowSource) *Registry {
	return &Registry{store: store, clients: clients, catalog: catalog, workflows: workflows}
}

func (r *Registry) List(ctx context.Context) ([]Record, error) {
	return r.store.ListPlugins(ctx)
}

func (r *Registry) fetch(ctx context.Context, name, baseURL string) (pluginapi.Manifest, Status, string, error) {
	pc, err := r.clients.For(name, baseURL)
	if err != nil {
		return pluginapi.Manifest{}, "", "", err
	}
	m, err := pc.Manifest(ctx)
	if err != nil {
		return pluginapi.Manifest{}, "", "", fmt.Errorf("%w: %s: %w", ErrUnreachable, name, err)
	}
	if err := m.Validate(); err != nil {
		return pluginapi.Manifest{}, "", "", err
	}
	if m.Name != name {
		return pluginapi.Manifest{}, "", "", fmt.Errorf("%w: %q registered as %q", ErrNameMismatch, m.Name, name)
	}
	health, err := pc.Health(ctx)
	if err != nil {
		return m, StatusUnreachable, err.Error(), nil
	}
	switch health.Status {
	case pluginapi.HealthOK:
		return m, StatusOK, "", nil
	case pluginapi.HealthDegraded:
		return m, StatusDegraded, "", nil
	}
	return m, StatusDown, "", nil
}

func (r *Registry) Register(ctx context.Context, name, baseURL string) (Record, error) {
	m, status, message, err := r.fetch(ctx, name, baseURL)
	if err != nil {
		return Record{}, err
	}
	rec := Record{Name: name, BaseURL: baseURL, Manifest: m, Status: status, Error: message, CheckedAt: time.Now().UTC()}
	if err := r.store.UpsertPlugin(ctx, rec); err != nil {
		return Record{}, fmt.Errorf("upsert plugin %s: %w", name, err)
	}
	if err := r.catalog.Refresh(ctx); err != nil {
		return Record{}, err
	}
	stored, err := r.store.GetPlugin(ctx, name)
	if err != nil {
		return Record{}, fmt.Errorf("get plugin %s: %w", name, err)
	}
	slog.InfoContext(ctx, "plugin registered", slog.String("name", name), slog.String("status", string(stored.Status)))
	return stored, nil
}

func (r *Registry) Refresh(ctx context.Context, name string) (Record, error) {
	rec, err := r.store.GetPlugin(ctx, name)
	if err != nil {
		return Record{}, fmt.Errorf("get plugin %s: %w", name, err)
	}
	m, status, message, err := r.fetch(ctx, name, rec.BaseURL)
	if err != nil {
		slog.WarnContext(ctx, "plugin refresh failed", slog.String("name", name), slog.Any("error", err))
		rec.Status = StatusUnreachable
		rec.Error = err.Error()
	} else {
		rec.Manifest = m
		rec.Status = status
		rec.Error = message
	}
	rec.CheckedAt = time.Now().UTC()
	if err := r.store.UpsertPlugin(ctx, rec); err != nil {
		return Record{}, fmt.Errorf("upsert plugin %s: %w", name, err)
	}
	if err := r.catalog.Refresh(ctx); err != nil {
		return Record{}, err
	}
	stored, err := r.store.GetPlugin(ctx, name)
	if err != nil {
		return Record{}, fmt.Errorf("get plugin %s: %w", name, err)
	}
	return stored, nil
}

func (r *Registry) RefreshAll(ctx context.Context) error {
	records, err := r.store.ListPlugins(ctx)
	if err != nil {
		return fmt.Errorf("list plugins: %w", err)
	}
	var errs []error
	for _, rec := range records {
		if _, err := r.Refresh(ctx, rec.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) Remove(ctx context.Context, name string) error {
	versions, err := r.workflows.LatestWorkflowVersions(ctx)
	if err != nil {
		return fmt.Errorf("list workflows: %w", err)
	}
	for _, v := range versions {
		var def workflow.Definition
		if err := json.Unmarshal(v.Definition, &def); err != nil {
			return fmt.Errorf("decode workflow %s: %w", v.Name, err)
		}
		if def.Trigger.Plugin == name {
			return fmt.Errorf("%w: workflow %s triggers on %s", ErrInUse, v.Name, name)
		}
		for id, s := range def.Steps {
			if strings.HasPrefix(s.Uses, name+"/") {
				return fmt.Errorf("%w: workflow %s step %s uses %s", ErrInUse, v.Name, id, s.Uses)
			}
		}
	}
	deleted, err := r.store.DeletePlugin(ctx, name)
	if err != nil {
		return fmt.Errorf("delete plugin %s: %w", name, err)
	}
	if !deleted {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err := r.catalog.Refresh(ctx); err != nil {
		return err
	}
	slog.InfoContext(ctx, "plugin removed", slog.String("name", name))
	return nil
}
