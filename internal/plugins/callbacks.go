package plugins

import (
	"context"
	"fmt"

	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type StepRunReader interface {
	GetStepRun(ctx context.Context, id string) (process.StepRun, error)
}

type JobSink interface {
	CompleteJob(ctx context.Context, res engine.JobResult) error
	JobProgress(ctx context.Context, p engine.JobProgress) error
}

type Callbacks struct {
	runs    StepRunReader
	catalog *Catalog
	sink    JobSink
}

func NewCallbacks(runs StepRunReader, catalog *Catalog, sink JobSink) *Callbacks {
	return &Callbacks{runs: runs, catalog: catalog, sink: sink}
}

func (c *Callbacks) Result(ctx context.Context, jobID string, res pluginapi.Result) error {
	if res.JobID != "" && res.JobID != jobID {
		return fmt.Errorf("%w: result job_id %q does not match %q", pluginapi.ErrInvalidInput, res.JobID, jobID)
	}
	run, err := c.runs.GetStepRun(ctx, jobID)
	if err != nil {
		return err
	}
	if run.Plugin == "" {
		return ErrNotPluginStep
	}
	_, spec, ok := c.catalog.Action(run.Uses)
	jr := engine.JobResult{StepRunID: jobID, Usage: toUsage(res.Usage)}
	switch {
	case !ok:
		jr.Error = fmt.Sprintf("action %s no longer registered", run.Uses)
		jr.ErrorKind = process.ErrorKindInternal
	case res.Error != nil:
		jr.Error = res.Error.Message
		jr.ErrorKind = errorKind(res.Error.Kind)
	default:
		if err := checkResult(spec, res); err != nil {
			jr.Error = err.Error()
			jr.ErrorKind = process.ErrorKindInternal
			break
		}
		jr.Outcome = step.Outcome(res.Outcome)
		jr.Output = res.Output
	}
	return c.sink.CompleteJob(ctx, jr)
}

func (c *Callbacks) Progress(ctx context.Context, jobID string, p pluginapi.Progress) error {
	jp := engine.JobProgress{StepRunID: jobID, Phase: p.Phase}
	if p.Usage != nil {
		usage := toUsage(*p.Usage)
		jp.Usage = &usage
	}
	return c.sink.JobProgress(ctx, jp)
}
