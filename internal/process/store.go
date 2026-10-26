package process

import (
	"context"
	"database/sql"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/events"
)

//go:generate moq -rm -out store_mock.go . Store:MockStore Tx:MockTx Querier:MockQuerier

type Querier interface {
	CreateProcess(ctx context.Context, p Process) (bool, error)
	GetProcess(ctx context.Context, id string) (Process, error)
	LockProcess(ctx context.Context, id string) (Process, error)
	ListProcesses(ctx context.Context, filter ListFilter) (Page, error)
	ListWaiting(ctx context.Context, kind WaitKind) ([]Process, error)
	SetNext(ctx context.Context, processID string, next Next) (bool, error)
	SetWaiting(ctx context.Context, processID string, wait Wait) (bool, error)
	FinishProcess(ctx context.Context, processID string, finish Finish) (bool, error)
	MergeVars(ctx context.Context, processID string, vars Vars) error

	StartStepRun(ctx context.Context, run StepRun) error
	FinishStepRun(ctx context.Context, result StepResult) (bool, error)
	GetStepRun(ctx context.Context, id string) (StepRun, error)
	ListStepRuns(ctx context.Context, processID string) ([]StepRun, error)
	ActiveStepRun(ctx context.Context, processID string) (StepRun, bool, error)
	SetStepPhase(ctx context.Context, stepRunID, phase string) error
	SetStepIteration(ctx context.Context, stepRunID string, iteration int) error
	SetStepSession(ctx context.Context, stepRunID, session string) error
	AddStepUsage(ctx context.Context, stepRunID string, usage events.TokenUsage) error
	TouchStepHeartbeat(ctx context.Context, stepRunID string) (bool, error)
	ListStaleJobs(ctx context.Context, now time.Time) ([]StaleJob, error)

	UpsertWorkflowVersion(ctx context.Context, v WorkflowVersion) (WorkflowVersion, error)
	GetWorkflowVersion(ctx context.Context, id int64) (WorkflowVersion, error)
	LatestWorkflowVersions(ctx context.Context) ([]WorkflowVersion, error)
	ListWorkflowVersions(ctx context.Context, name string) ([]WorkflowVersion, error)
	SetCurrentWorkflow(ctx context.Context, name string, versionID int64) error
	SeedWorkflow(ctx context.Context, name string, versionID int64) (bool, error)
	DeleteWorkflow(ctx context.Context, name string) (bool, error)
}

type Tx interface {
	Querier
	SQL() *sql.Tx
}

type Store interface {
	Querier
	InTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
}
