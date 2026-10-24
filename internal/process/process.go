package process

import (
	"encoding/json"
	"time"

	"ai-orchestration/internal/events"
)

type processError string

func (e processError) Error() string { return string(e) }

const (
	ErrNotFound      processError = "process not found"
	ErrNotActive     processError = "process is not active"
	ErrInvalidStatus processError = "invalid status"
	ErrInvalidKind   processError = "invalid kind"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusWaiting   Status = "waiting"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusPending, StatusRunning, StatusWaiting, StatusSucceeded, StatusFailed, StatusCancelled:
		return Status(s), nil
	}
	return "", ErrInvalidStatus
}

func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

func RetryableStatuses() []Status {
	return []Status{StatusFailed, StatusCancelled}
}

type StepStatus string

const (
	StepRunning   StepStatus = "running"
	StepSucceeded StepStatus = "succeeded"
	StepFailed    StepStatus = "failed"
	StepCancelled StepStatus = "cancelled"
)

func ParseStepStatus(s string) (StepStatus, error) {
	switch StepStatus(s) {
	case StepRunning, StepSucceeded, StepFailed, StepCancelled:
		return StepStatus(s), nil
	}
	return "", ErrInvalidStatus
}

type ErrorKind string

const (
	ErrorKindNone     ErrorKind = ""
	ErrorKindUpstream ErrorKind = "upstream"
	ErrorKindInternal ErrorKind = "internal"
	ErrorKindBusiness ErrorKind = "business"
)

func ParseErrorKind(s string) (ErrorKind, error) {
	switch ErrorKind(s) {
	case ErrorKindNone, ErrorKindUpstream, ErrorKindInternal, ErrorKindBusiness:
		return ErrorKind(s), nil
	}
	return "", ErrInvalidKind
}

type WaitKind string

const (
	WaitComment WaitKind = "comment"
	WaitSignal  WaitKind = "signal"
)

func ParseWaitKind(s string) (WaitKind, error) {
	switch WaitKind(s) {
	case WaitComment, WaitSignal:
		return WaitKind(s), nil
	}
	return "", ErrInvalidKind
}

type SubjectKind string

const SubjectIssue SubjectKind = "issue"

type Subject struct {
	Kind SubjectKind `json:"kind"`
	Key  string      `json:"key"`
	URL  string      `json:"url,omitempty"`
}

type Wait struct {
	Kind          WaitKind  `json:"kind"`
	Step          string    `json:"step"`
	Since         time.Time `json:"since"`
	ResumeOutcome string    `json:"resume_outcome"`
}

type Process struct {
	ID                string            `json:"id"`
	WorkflowVersionID int64             `json:"workflow_version_id"`
	WorkflowName      string            `json:"workflow_name"`
	Subject           Subject           `json:"subject"`
	Title             string            `json:"title"`
	Project           string            `json:"project,omitempty"`
	Status            Status            `json:"status"`
	CurrentStep       string            `json:"current_step,omitempty"`
	CurrentJobID      *int64            `json:"current_job_id,omitempty"`
	Vars              Vars              `json:"vars"`
	WaitingFor        *Wait             `json:"waiting_for,omitempty"`
	Error             string            `json:"error,omitempty"`
	ErrorKind         ErrorKind         `json:"error_kind,omitempty"`
	Usage             events.TokenUsage `json:"usage"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	StartedAt         *time.Time        `json:"started_at,omitempty"`
	EndedAt           *time.Time        `json:"ended_at,omitempty"`
}

type StepRun struct {
	ID            string            `json:"id"`
	ProcessID     string            `json:"process_id"`
	StepID        string            `json:"step_id"`
	Uses          string            `json:"uses"`
	Attempt       int               `json:"attempt"`
	Iteration     int               `json:"iteration"`
	RiverJobID    int64             `json:"river_job_id"`
	WorkerID      string            `json:"worker_id"`
	Status        StepStatus        `json:"status"`
	Outcome       string            `json:"outcome,omitempty"`
	Phase         string            `json:"phase,omitempty"`
	Error         string            `json:"error,omitempty"`
	ErrorKind     ErrorKind         `json:"error_kind,omitempty"`
	Usage         events.TokenUsage `json:"usage"`
	ClaudeSession string            `json:"claude_session,omitempty"`
	Output        Vars              `json:"output,omitempty"`
	StartedAt     time.Time         `json:"started_at"`
	EndedAt       *time.Time        `json:"ended_at,omitempty"`
}

type StepResult struct {
	StepRunID string
	Status    StepStatus
	Outcome   string
	Error     string
	ErrorKind ErrorKind
	Output    Vars
}

type WorkflowVersion struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Hash       string          `json:"hash"`
	Definition json.RawMessage `json:"definition"`
	SourceYAML string          `json:"source_yaml"`
	CreatedAt  time.Time       `json:"created_at"`
}

type Finish struct {
	Status    Status
	Error     string
	ErrorKind ErrorKind
}

type Next struct {
	From  []Status
	Step  string
	JobID int64
}

type ListFilter struct {
	Status       Status
	WorkflowName string
	Project      string
	Limit        int
	Cursor       *Cursor
}

type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

type Page struct {
	Processes []Process `json:"processes"`
	Next      *Cursor   `json:"next,omitempty"`
}
