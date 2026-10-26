package plugins

import (
	"context"
	"strings"
	"time"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

//go:generate moq -rm -out plugins_mock.go . Store:MockStore WorkflowSource:MockWorkflowSource StepRunReader:MockStepRunReader JobSink:MockJobSink StaleLister:MockStaleLister

type pluginError string

func (e pluginError) Error() string { return string(e) }

const (
	ErrNotFound         pluginError = "plugin not found"
	ErrNoToken          pluginError = "plugin token not configured"
	ErrNameMismatch     pluginError = "manifest name does not match plugin name"
	ErrInUse            pluginError = "plugin is used by a workflow"
	ErrUnauthorized     pluginError = "unauthorized plugin request"
	ErrUnknownTrigger   pluginError = "unknown plugin trigger"
	ErrInvalidPayload   pluginError = "trigger payload does not match schema"
	ErrInvalidSignature pluginError = "invalid callback signature"
	ErrExpiredSignature pluginError = "callback signature expired"
	ErrNotPluginStep    pluginError = "step run is not a plugin job"
	ErrUnreachable      pluginError = "plugin unreachable"
	ErrJobFailed        pluginError = "plugin job failed"
	ErrInvalidResult    pluginError = "plugin result violates the action contract"
)

const (
	StepQueue               = "default"
	DefaultHeartbeatSeconds = 30
	DefaultJobTimeout       = time.Hour
	SignatureGrace          = 15 * time.Minute
)

type Status string

const (
	StatusOK          Status = "ok"
	StatusDegraded    Status = "degraded"
	StatusDown        Status = "down"
	StatusUnreachable Status = "unreachable"
)

type Record struct {
	Name      string             `json:"name"`
	BaseURL   string             `json:"base_url"`
	Manifest  pluginapi.Manifest `json:"manifest"`
	Status    Status             `json:"status"`
	Error     string             `json:"error,omitempty"`
	CheckedAt time.Time          `json:"checked_at"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type Store interface {
	UpsertPlugin(ctx context.Context, r Record) error
	GetPlugin(ctx context.Context, name string) (Record, error)
	ListPlugins(ctx context.Context) ([]Record, error)
	DeletePlugin(ctx context.Context, name string) (bool, error)
}

func TokenEnv(name string) string {
	return "PLUGIN_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_TOKEN"
}

func SplitUses(uses string) (string, string, bool) {
	plugin, action, found := strings.Cut(uses, "/")
	if !found || plugin == "" || action == "" {
		return "", "", false
	}
	return plugin, action, true
}
