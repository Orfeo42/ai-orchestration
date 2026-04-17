package runstore

import (
	"time"

	"ai-orchestration/internal/events"
)

type Service string

const (
	ServiceOrchestrator Service = "orchestrator"
	ServiceReviewer     Service = "reviewer"
	ServiceAssess       Service = "assess"
	ServiceImpl         Service = "impl"
	ServiceReview       Service = "review"
)

type Phase string

const (
	PhaseAssessing    Phase = "assessing"
	PhaseImplementing Phase = "implementing"
	PhaseTesting      Phase = "testing"
	PhasePushing      Phase = "pushing"
	PhasePRDesc       Phase = "pr_description"
	PhaseReviewing    Phase = "reviewing"
	PhaseDone         Phase = "done"
	PhaseFailed       Phase = "failed"
	PhaseCancelled    Phase = "cancelled"
)

type EventTail struct {
	Events []events.Event
	LastID string
}

func (p Phase) Terminal() bool {
	switch p {
	case PhaseDone, PhaseFailed, PhaseCancelled:
		return true
	}
	return false
}

type Run struct {
	ID            string            `json:"id"`
	Service       Service           `json:"service"`
	RepoOwner     string            `json:"repo_owner"`
	RepoName      string            `json:"repo_name"`
	IssueNumber   int               `json:"issue_number,omitempty"`
	PRNumber      int               `json:"pr_number,omitempty"`
	Title         string            `json:"title"`
	Project       string            `json:"project,omitempty"`
	Phase         Phase             `json:"phase"`
	Attempt       int               `json:"attempt"`
	StartedAt     time.Time         `json:"started_at"`
	EndedAt       *time.Time        `json:"ended_at,omitempty"`
	Usage         events.TokenUsage `json:"usage"`
	Error         string            `json:"error,omitempty"`
	ClaudeSession string            `json:"claude_session,omitempty"`
	InjectNotes   []string          `json:"inject_notes,omitempty"`
}

type ListFilter struct {
	Service Service
	Phase   Phase
	Limit   int
	Cursor  int64
}
