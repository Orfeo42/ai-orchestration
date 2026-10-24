package plugins

import (
	"encoding/json"
	"fmt"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

func toUsage(u pluginapi.Usage) events.TokenUsage {
	return events.TokenUsage{
		Input:              u.Input,
		Output:             u.Output,
		CacheCreationInput: u.CacheCreationInput,
		CacheReadInput:     u.CacheReadInput,
	}
}

func errorKind(k pluginapi.ErrorKind) process.ErrorKind {
	if k == pluginapi.ErrorUpstream {
		return process.ErrorKindUpstream
	}
	return process.ErrorKindInternal
}

func checkResult(spec pluginapi.ActionSpec, res pluginapi.Result) error {
	if res.Error != nil {
		return nil
	}
	if !spec.Declares(res.Outcome) {
		return fmt.Errorf("%w: undeclared outcome %q", ErrInvalidResult, res.Outcome)
	}
	schema, err := pluginapi.CompileSchema(spec.OutputSchema)
	if err != nil {
		return fmt.Errorf("%w: output schema: %w", ErrInvalidResult, err)
	}
	output := res.Output
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	if err := schema.Validate(output); err != nil {
		return fmt.Errorf("%w: output: %w", ErrInvalidResult, err)
	}
	return nil
}
