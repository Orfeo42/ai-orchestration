package analytics

import (
	"context"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
)

//go:generate moq -rm -out analytics_mock.go . Querier:MockQuerier

type analyticsError string

func (e analyticsError) Error() string { return string(e) }

const ErrInvalidGrouping analyticsError = "invalid grouping"

type Grouping string

const (
	GroupDay     Grouping = "day"
	GroupProject Grouping = "project"
	GroupStep    Grouping = "step"
)

func ParseGrouping(s string) (Grouping, error) {
	switch Grouping(s) {
	case GroupDay, GroupProject, GroupStep:
		return Grouping(s), nil
	}
	return "", ErrInvalidGrouping
}

type Range struct {
	From time.Time
	To   time.Time
}

type WorkflowStats struct {
	Workflow      string            `json:"workflow"`
	Processes     int               `json:"processes"`
	Active        int               `json:"active"`
	Succeeded     int               `json:"succeeded"`
	Failed        int               `json:"failed"`
	Cancelled     int               `json:"cancelled"`
	SuccessRate   float64           `json:"success_rate"`
	DurationP50Ms int64             `json:"duration_p50_ms"`
	DurationP95Ms int64             `json:"duration_p95_ms"`
	Usage         events.TokenUsage `json:"usage"`
}

type StepStats struct {
	Workflow      string            `json:"workflow"`
	StepID        string            `json:"step_id"`
	Uses          string            `json:"uses"`
	Runs          int               `json:"runs"`
	Outcomes      map[string]int    `json:"outcomes"`
	FailureRate   float64           `json:"failure_rate"`
	Retries       int               `json:"retries"`
	DurationP50Ms int64             `json:"duration_p50_ms"`
	DurationP95Ms int64             `json:"duration_p95_ms"`
	Usage         events.TokenUsage `json:"usage"`
}

type TokenPoint struct {
	Bucket string            `json:"bucket"`
	Usage  events.TokenUsage `json:"usage"`
}

type FailureGroup struct {
	ErrorKind process.ErrorKind `json:"error_kind"`
	Error     string            `json:"error"`
	Count     int               `json:"count"`
	LastAt    time.Time         `json:"last_at"`
}

type Querier interface {
	Workflows(ctx context.Context, r Range) ([]WorkflowStats, error)
	Steps(ctx context.Context, workflow string, r Range) ([]StepStats, error)
	Tokens(ctx context.Context, r Range, g Grouping) ([]TokenPoint, error)
	Failures(ctx context.Context, r Range) ([]FailureGroup, error)
}
