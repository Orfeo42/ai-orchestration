package pluginapi

import (
	"encoding/json"
	"time"
)

type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

type ErrorKind string

const (
	ErrorUpstream ErrorKind = "upstream"
	ErrorInternal ErrorKind = "internal"
)

type Callback struct {
	ResultURL   string `json:"result_url"`
	ProgressURL string `json:"progress_url"`
}

type JobRequest struct {
	JobID            string          `json:"job_id"`
	ProcessID        string          `json:"process_id"`
	Input            json.RawMessage `json:"input"`
	Callback         Callback        `json:"callback"`
	Deadline         time.Time       `json:"deadline"`
	HeartbeatSeconds int             `json:"heartbeat_seconds"`
}

type Usage struct {
	Input              int `json:"input"`
	Output             int `json:"output"`
	CacheCreationInput int `json:"cache_creation_input"`
	CacheReadInput     int `json:"cache_read_input"`
}

type JobError struct {
	Kind    ErrorKind `json:"kind"`
	Message string    `json:"message"`
}

type Result struct {
	JobID   string          `json:"job_id"`
	Outcome Outcome         `json:"outcome,omitempty"`
	Output  json.RawMessage `json:"output,omitempty"`
	Error   *JobError       `json:"error,omitempty"`
	Usage   Usage           `json:"usage"`
}

type JobState struct {
	JobID     string    `json:"job_id"`
	Action    string    `json:"action"`
	Status    JobStatus `json:"status"`
	Phase     string    `json:"phase,omitempty"`
	Result    *Result   `json:"result,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Progress struct {
	JobID   string    `json:"job_id"`
	Phase   string    `json:"phase"`
	Message string    `json:"message,omitempty"`
	Usage   *Usage    `json:"usage,omitempty"`
	At      time.Time `json:"at"`
}

type Event struct {
	Seq  int64           `json:"seq"`
	At   time.Time       `json:"at"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

type Message struct {
	Text string `json:"text"`
}

type HealthStatus string

const (
	HealthOK       HealthStatus = "ok"
	HealthDegraded HealthStatus = "degraded"
	HealthDown     HealthStatus = "down"
)

type Check struct {
	Status  HealthStatus `json:"status"`
	Message string       `json:"message,omitempty"`
}

type Health struct {
	Status HealthStatus     `json:"status"`
	Checks map[string]Check `json:"checks,omitempty"`
}
