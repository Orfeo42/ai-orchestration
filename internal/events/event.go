package events

import (
	"encoding/json"
	"time"
)

type Kind string

const (
	KindRunStart   Kind = "run_start"
	KindPhase      Kind = "phase"
	KindAssistant  Kind = "assistant_text"
	KindToolUse    Kind = "tool_use"
	KindToolResult Kind = "tool_result"
	KindThinking   Kind = "thinking"
	KindResult     Kind = "result"
	KindRunEnd     Kind = "run_end"
	KindCancel     Kind = "cancel"
	KindInject     Kind = "inject"
	KindError      Kind = "error"
	KindInit       Kind = "init"
)

type TokenUsage struct {
	Input              int `json:"input"`
	Output             int `json:"output"`
	CacheCreationInput int `json:"cache_creation_input"`
	CacheReadInput     int `json:"cache_read_input"`
}

func (u TokenUsage) Add(o TokenUsage) TokenUsage {
	return TokenUsage{
		Input:              u.Input + o.Input,
		Output:             u.Output + o.Output,
		CacheCreationInput: u.CacheCreationInput + o.CacheCreationInput,
		CacheReadInput:     u.CacheReadInput + o.CacheReadInput,
	}
}

type Event struct {
	ID         string          `json:"id,omitempty"`
	RunID      string          `json:"run_id"`
	Seq        int64           `json:"seq,omitempty"`
	Kind       Kind            `json:"kind"`
	At         time.Time       `json:"at"`
	Text       string          `json:"text,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  string          `json:"tool_input,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Phase      string          `json:"phase,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	Service    string          `json:"service,omitempty"`
	Usage      *TokenUsage     `json:"usage,omitempty"`
	ResultText string          `json:"result_text,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
}
