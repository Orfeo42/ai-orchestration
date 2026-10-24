package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
)

const emptyJSON = "{}"

type processRow struct {
	ID                 string     `db:"id"`
	WorkflowVersionID  int64      `db:"workflow_version_id"`
	WorkflowName       string     `db:"workflow_name"`
	SubjectKind        string     `db:"subject_kind"`
	SubjectKey         string     `db:"subject_key"`
	SubjectURL         string     `db:"subject_url"`
	Title              string     `db:"title"`
	Project            string     `db:"project"`
	Status             string     `db:"status"`
	CurrentStep        string     `db:"current_step"`
	CurrentJobID       *int64     `db:"current_job_id"`
	Vars               string     `db:"vars"`
	WaitingFor         *string    `db:"waiting_for"`
	Error              string     `db:"error"`
	ErrorKind          string     `db:"error_kind"`
	UsageInput         int64      `db:"usage_input"`
	UsageOutput        int64      `db:"usage_output"`
	UsageCacheCreation int64      `db:"usage_cache_creation"`
	UsageCacheRead     int64      `db:"usage_cache_read"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
	StartedAt          *time.Time `db:"started_at"`
	EndedAt            *time.Time `db:"ended_at"`
}

type stepRunRow struct {
	ID                  string     `db:"id"`
	ProcessID           string     `db:"process_id"`
	StepID              string     `db:"step_id"`
	Uses                string     `db:"uses"`
	Attempt             int        `db:"attempt"`
	Iteration           int        `db:"iteration"`
	RiverJobID          int64      `db:"river_job_id"`
	WorkerID            string     `db:"worker_id"`
	Status              string     `db:"status"`
	Outcome             string     `db:"outcome"`
	Phase               string     `db:"phase"`
	Error               string     `db:"error"`
	ErrorKind           string     `db:"error_kind"`
	TokensInput         int64      `db:"tokens_input"`
	TokensOutput        int64      `db:"tokens_output"`
	TokensCacheCreation int64      `db:"tokens_cache_creation"`
	TokensCacheRead     int64      `db:"tokens_cache_read"`
	ClaudeSession       string     `db:"claude_session"`
	Output              string     `db:"output"`
	StartedAt           time.Time  `db:"started_at"`
	EndedAt             *time.Time `db:"ended_at"`
}

type workflowVersionRow struct {
	ID         int64     `db:"id"`
	Name       string    `db:"name"`
	Hash       string    `db:"hash"`
	Definition string    `db:"definition"`
	SourceYAML string    `db:"source_yaml"`
	CreatedAt  time.Time `db:"created_at"`
}

func encodeVars(v process.Vars) (string, error) {
	if v == nil {
		return emptyJSON, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode vars: %w", err)
	}
	return string(raw), nil
}

func decodeVars(raw string) (process.Vars, error) {
	vars := process.Vars{}
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		return nil, fmt.Errorf("decode vars: %w", err)
	}
	return vars, nil
}

func encodeWait(w process.Wait) (string, error) {
	raw, err := json.Marshal(w)
	if err != nil {
		return "", fmt.Errorf("encode wait: %w", err)
	}
	return string(raw), nil
}

func decodeWait(raw string) (process.Wait, error) {
	var w process.Wait
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		return process.Wait{}, fmt.Errorf("decode wait: %w", err)
	}
	if _, err := process.ParseWaitKind(string(w.Kind)); err != nil {
		return process.Wait{}, fmt.Errorf("decode wait: %w", err)
	}
	return w, nil
}

func fromProcess(p process.Process) (processRow, error) {
	vars, err := encodeVars(p.Vars)
	if err != nil {
		return processRow{}, err
	}
	row := processRow{
		ID:                p.ID,
		WorkflowVersionID: p.WorkflowVersionID,
		WorkflowName:      p.WorkflowName,
		SubjectKind:       string(p.Subject.Kind),
		SubjectKey:        p.Subject.Key,
		SubjectURL:        p.Subject.URL,
		Title:             p.Title,
		Project:           p.Project,
		Status:            string(p.Status),
		CurrentStep:       p.CurrentStep,
		CurrentJobID:      p.CurrentJobID,
		Vars:              vars,
		Error:             p.Error,
		ErrorKind:         string(p.ErrorKind),
	}
	if p.WaitingFor == nil {
		return row, nil
	}
	wait, err := encodeWait(*p.WaitingFor)
	if err != nil {
		return processRow{}, err
	}
	row.WaitingFor = &wait
	return row, nil
}

func toProcess(r processRow) (process.Process, error) {
	status, err := process.ParseStatus(r.Status)
	if err != nil {
		return process.Process{}, fmt.Errorf("process %s: %w", r.ID, err)
	}
	errorKind, err := process.ParseErrorKind(r.ErrorKind)
	if err != nil {
		return process.Process{}, fmt.Errorf("process %s: %w", r.ID, err)
	}
	vars, err := decodeVars(r.Vars)
	if err != nil {
		return process.Process{}, fmt.Errorf("process %s: %w", r.ID, err)
	}
	p := process.Process{
		ID:                r.ID,
		WorkflowVersionID: r.WorkflowVersionID,
		WorkflowName:      r.WorkflowName,
		Subject:           process.Subject{Kind: process.SubjectKind(r.SubjectKind), Key: r.SubjectKey, URL: r.SubjectURL},
		Title:             r.Title,
		Project:           r.Project,
		Status:            status,
		CurrentStep:       r.CurrentStep,
		CurrentJobID:      r.CurrentJobID,
		Vars:              vars,
		Error:             r.Error,
		ErrorKind:         errorKind,
		Usage: events.TokenUsage{
			Input:              int(r.UsageInput),
			Output:             int(r.UsageOutput),
			CacheCreationInput: int(r.UsageCacheCreation),
			CacheReadInput:     int(r.UsageCacheRead),
		},
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		StartedAt: r.StartedAt,
		EndedAt:   r.EndedAt,
	}
	if r.WaitingFor == nil {
		return p, nil
	}
	wait, err := decodeWait(*r.WaitingFor)
	if err != nil {
		return process.Process{}, fmt.Errorf("process %s: %w", r.ID, err)
	}
	p.WaitingFor = &wait
	return p, nil
}

func toProcesses(rows []processRow) ([]process.Process, error) {
	out := make([]process.Process, 0, len(rows))
	for _, r := range rows {
		p, err := toProcess(r)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func toStepRun(r stepRunRow) (process.StepRun, error) {
	status, err := process.ParseStepStatus(r.Status)
	if err != nil {
		return process.StepRun{}, fmt.Errorf("step run %s: %w", r.ID, err)
	}
	errorKind, err := process.ParseErrorKind(r.ErrorKind)
	if err != nil {
		return process.StepRun{}, fmt.Errorf("step run %s: %w", r.ID, err)
	}
	output, err := decodeVars(r.Output)
	if err != nil {
		return process.StepRun{}, fmt.Errorf("step run %s: %w", r.ID, err)
	}
	return process.StepRun{
		ID:         r.ID,
		ProcessID:  r.ProcessID,
		StepID:     r.StepID,
		Uses:       r.Uses,
		Attempt:    r.Attempt,
		Iteration:  r.Iteration,
		RiverJobID: r.RiverJobID,
		WorkerID:   r.WorkerID,
		Status:     status,
		Outcome:    r.Outcome,
		Phase:      r.Phase,
		Error:      r.Error,
		ErrorKind:  errorKind,
		Usage: events.TokenUsage{
			Input:              int(r.TokensInput),
			Output:             int(r.TokensOutput),
			CacheCreationInput: int(r.TokensCacheCreation),
			CacheReadInput:     int(r.TokensCacheRead),
		},
		ClaudeSession: r.ClaudeSession,
		Output:        output,
		StartedAt:     r.StartedAt,
		EndedAt:       r.EndedAt,
	}, nil
}

func toStepRuns(rows []stepRunRow) ([]process.StepRun, error) {
	out := make([]process.StepRun, 0, len(rows))
	for _, r := range rows {
		s, err := toStepRun(r)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func toWorkflowVersion(r workflowVersionRow) process.WorkflowVersion {
	return process.WorkflowVersion{
		ID:         r.ID,
		Name:       r.Name,
		Hash:       r.Hash,
		Definition: json.RawMessage(r.Definition),
		SourceYAML: r.SourceYAML,
		CreatedAt:  r.CreatedAt,
	}
}

func toWorkflowVersions(rows []workflowVersionRow) []process.WorkflowVersion {
	out := make([]process.WorkflowVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, toWorkflowVersion(r))
	}
	return out
}
