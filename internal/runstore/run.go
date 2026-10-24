package runstore

import (
	"github.com/Orfeo42/ai-orchestration/internal/events"
)

type Service string

const (
	ServiceOrchestrator Service = "orchestrator"
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
