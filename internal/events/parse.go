package events

import (
	"encoding/json"
	"strings"
	"time"
)

type rawEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Message   *rawMessage     `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	Usage     *rawUsage       `json:"usage,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type rawMessage struct {
	ID      string       `json:"id,omitempty"`
	Content []rawContent `json:"content,omitempty"`
	Usage   *rawUsage    `json:"usage,omitempty"`
}

type rawContent struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	Thinking string          `json:"thinking,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	ID       string          `json:"id,omitempty"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func usageFrom(r *rawUsage) *TokenUsage {
	if r == nil {
		return nil
	}
	return &TokenUsage{
		Input:              r.InputTokens,
		Output:             r.OutputTokens,
		CacheCreationInput: r.CacheCreationInputTokens,
		CacheReadInput:     r.CacheReadInputTokens,
	}
}

func ParseClaudeLine(line string) []Event {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var raw rawEvent
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return []Event{{Kind: KindAssistant, Text: line, At: now()}}
	}

	switch raw.Type {
	case "system":
		if raw.Subtype == "init" && raw.SessionID != "" {
			return []Event{{Kind: KindInit, SessionID: raw.SessionID, At: now()}}
		}
	case "assistant":
		return parseAssistant(raw)
	case "user", "tool_result":
		return parseToolResults(raw)
	case "result":
		evt := Event{
			Kind:       KindResult,
			ResultText: raw.Result,
			SessionID:  raw.SessionID,
			Usage:      usageFrom(raw.Usage),
			At:         now(),
		}
		return []Event{evt}
	}
	return nil
}

func parseAssistant(raw rawEvent) []Event {
	if raw.Message == nil {
		return nil
	}
	var out []Event
	for _, c := range raw.Message.Content {
		switch c.Type {
		case "text":
			if c.Text == "" {
				continue
			}
			out = append(out, Event{Kind: KindAssistant, Text: c.Text, At: now()})
		case "thinking":
			if c.Thinking == "" {
				continue
			}
			out = append(out, Event{Kind: KindThinking, Text: c.Thinking, At: now()})
		case "tool_use":
			out = append(out, Event{
				Kind:      KindToolUse,
				ToolName:  c.Name,
				ToolID:    c.ID,
				ToolInput: string(c.Input),
				At:        now(),
			})
		}
	}
	if u := usageFrom(raw.Message.Usage); u != nil {
		out = append(out, Event{Kind: KindResult, Usage: u, At: now()})
	}
	return out
}

func parseToolResults(raw rawEvent) []Event {
	var out []Event
	if raw.Message != nil {
		for _, c := range raw.Message.Content {
			if c.Type != "tool_result" {
				continue
			}
			out = append(out, Event{
				Kind:   KindToolResult,
				ToolID: c.ID,
				Text:   extractToolResultText(c),
				At:     now(),
			})
		}
	}
	if len(out) == 0 && len(raw.Content) > 0 {
		out = append(out, Event{
			Kind:   KindToolResult,
			ToolID: raw.ToolUseID,
			Text:   extractRawToolResultText(raw.Content),
			At:     now(),
		})
	}
	return out
}

func extractToolResultText(c rawContent) string {
	if c.Text != "" {
		return c.Text
	}
	if len(c.Input) > 0 {
		return extractRawToolResultText(c.Input)
	}
	return ""
}

func extractRawToolResultText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var arr []rawContent
	if err := json.Unmarshal(raw, &arr); err == nil {
		var b strings.Builder
		for i, item := range arr {
			if item.Type != "text" || item.Text == "" {
				continue
			}
			if i > 0 && b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(item.Text)
		}
		return b.String()
	}
	return string(raw)
}

var nowFn = time.Now

func now() time.Time { return nowFn().UTC() }
