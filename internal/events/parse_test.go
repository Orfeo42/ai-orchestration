package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClaudeLine(t *testing.T) {
	t.Run("empty line returns nil", func(t *testing.T) {
		assert.Nil(t, ParseClaudeLine(""))
		assert.Nil(t, ParseClaudeLine("   "))
	})

	t.Run("non-json line falls back to assistant text", func(t *testing.T) {
		got := ParseClaudeLine("plain text")
		require.Len(t, got, 1)
		assert.Equal(t, KindAssistant, got[0].Kind)
		assert.Equal(t, "plain text", got[0].Text)
	})

	t.Run("system init captures session id", func(t *testing.T) {
		got := ParseClaudeLine(`{"type":"system","subtype":"init","session_id":"sess-123"}`)
		require.Len(t, got, 1)
		assert.Equal(t, KindInit, got[0].Kind)
		assert.Equal(t, "sess-123", got[0].SessionID)
	})

	t.Run("assistant text", func(t *testing.T) {
		line := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 1)
		assert.Equal(t, KindAssistant, got[0].Kind)
		assert.Equal(t, "hello", got[0].Text)
	})

	t.Run("assistant tool_use", func(t *testing.T) {
		line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"file_path":"/x"}}]}}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 1)
		assert.Equal(t, KindToolUse, got[0].Kind)
		assert.Equal(t, "Read", got[0].ToolName)
		assert.Equal(t, "tu_1", got[0].ToolID)
		assert.JSONEq(t, `{"file_path":"/x"}`, got[0].ToolInput)
	})

	t.Run("assistant thinking block", func(t *testing.T) {
		line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"reasoning..."}]}}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 1)
		assert.Equal(t, KindThinking, got[0].Kind)
		assert.Equal(t, "reasoning...", got[0].Text)
	})

	t.Run("assistant message with usage emits result with tokens", func(t *testing.T) {
		line := `{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":5}}}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 2)
		assert.Equal(t, KindAssistant, got[0].Kind)
		assert.Equal(t, KindResult, got[1].Kind)
		require.NotNil(t, got[1].Usage)
		assert.Equal(t, 10, got[1].Usage.Input)
		assert.Equal(t, 5, got[1].Usage.Output)
	})

	t.Run("user tool_result string content", func(t *testing.T) {
		line := `{"type":"user","message":{"content":[{"type":"tool_result","id":"tu_1","text":"file contents"}]}}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 1)
		assert.Equal(t, KindToolResult, got[0].Kind)
		assert.Equal(t, "file contents", got[0].Text)
		assert.Equal(t, "tu_1", got[0].ToolID)
	})

	t.Run("legacy tool_result top-level string content", func(t *testing.T) {
		line := `{"type":"user","tool_use_id":"tu_2","content":"raw string"}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 1)
		assert.Equal(t, KindToolResult, got[0].Kind)
		assert.Equal(t, "raw string", got[0].Text)
		assert.Equal(t, "tu_2", got[0].ToolID)
	})

	t.Run("legacy tool_result top-level array content", func(t *testing.T) {
		line := `{"type":"user","tool_use_id":"tu_3","content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 1)
		assert.Equal(t, KindToolResult, got[0].Kind)
		assert.Equal(t, "line1\nline2", got[0].Text)
	})

	t.Run("result event captures usage and result text", func(t *testing.T) {
		line := `{"type":"result","subtype":"success","session_id":"sess-9","result":"done","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":7,"cache_read_input_tokens":3}}`
		got := ParseClaudeLine(line)
		require.Len(t, got, 1)
		assert.Equal(t, KindResult, got[0].Kind)
		assert.Equal(t, "done", got[0].ResultText)
		assert.Equal(t, "sess-9", got[0].SessionID)
		require.NotNil(t, got[0].Usage)
		assert.Equal(t, TokenUsage{Input: 100, Output: 50, CacheCreationInput: 7, CacheReadInput: 3}, *got[0].Usage)
	})

	t.Run("unknown type returns nil", func(t *testing.T) {
		assert.Nil(t, ParseClaudeLine(`{"type":"mystery"}`))
	})
}

func TestTokenUsageAdd(t *testing.T) {
	t.Run("sums all fields", func(t *testing.T) {
		a := TokenUsage{Input: 1, Output: 2, CacheCreationInput: 3, CacheReadInput: 4}
		b := TokenUsage{Input: 10, Output: 20, CacheCreationInput: 30, CacheReadInput: 40}
		assert.Equal(t, TokenUsage{Input: 11, Output: 22, CacheCreationInput: 33, CacheReadInput: 44}, a.Add(b))
	})
}
