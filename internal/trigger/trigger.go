package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"ai-orchestration/internal/engine"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/step"
	"ai-orchestration/internal/steps"
	"ai-orchestration/internal/workflow"
)

//go:generate moq -rm -out trigger_mock.go . Engine:MockEngine Store:MockStore

type triggerError string

func (e triggerError) Error() string { return string(e) }

const (
	ErrInvalidSubject  triggerError = "invalid subject key, expected owner/repo#number"
	ErrUnknownWorkflow triggerError = "unknown workflow"
	ErrNoProject       triggerError = "issue has no configured project label"
	ErrProcessExists   triggerError = "a process already exists for this workflow and issue"
)

type Engine interface {
	Start(ctx context.Context, req engine.StartRequest) (engine.StartResult, error)
	Resume(ctx context.Context, processID string) error
}

type Store interface {
	UpsertWorkflowVersion(ctx context.Context, v process.WorkflowVersion) (process.WorkflowVersion, error)
	LatestWorkflowVersions(ctx context.Context) ([]process.WorkflowVersion, error)
	ListWaiting(ctx context.Context, kind process.WaitKind) ([]process.Process, error)
}

type Config struct {
	ForgejoURL string
	Projects   map[string]projects.Config
	BotUser    string
}

type Service struct {
	fj    forgejo.Forgejo
	store Store
	eng   Engine
	reg   *step.Registry
	cfg   Config
}

func New(fj forgejo.Forgejo, store Store, eng Engine, reg *step.Registry, cfg Config) *Service {
	return &Service{fj: fj, store: store, eng: eng, reg: reg, cfg: cfg}
}

func (s *Service) SyncWorkflows(ctx context.Context, dir string) error {
	sources, err := workflow.LoadDir(dir, s.reg)
	if err != nil {
		return fmt.Errorf("load workflows: %w", err)
	}
	latest, err := s.store.LatestWorkflowVersions(ctx)
	if err != nil {
		return fmt.Errorf("latest workflow versions: %w", err)
	}
	previous := make(map[string]int64, len(latest))
	for _, v := range latest {
		previous[v.Name] = v.ID
	}
	for _, src := range sources {
		def, err := json.Marshal(src.Definition)
		if err != nil {
			return fmt.Errorf("encode workflow %s: %w", src.Definition.Name, err)
		}
		v, err := s.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{
			Name:       src.Definition.Name,
			Hash:       src.Hash,
			Definition: def,
			SourceYAML: string(src.YAML),
		})
		if err != nil {
			return fmt.Errorf("upsert workflow %s: %w", src.Definition.Name, err)
		}
		if v.ID != previous[v.Name] {
			slog.InfoContext(ctx, "workflow version registered",
				slog.String("workflow", v.Name),
				slog.Int64("version_id", v.ID),
				slog.String("hash", v.Hash),
			)
		}
	}
	return nil
}

func (s *Service) Poll(ctx context.Context) error {
	versions, err := s.store.LatestWorkflowVersions(ctx)
	if err != nil {
		return fmt.Errorf("latest workflow versions: %w", err)
	}
	for _, v := range versions {
		var def workflow.Definition
		if err := json.Unmarshal(v.Definition, &def); err != nil {
			slog.ErrorContext(ctx, "decode workflow definition failed",
				slog.String("workflow", v.Name),
				slog.Any("error", err),
			)
			continue
		}
		if def.Trigger.Forgejo == nil {
			continue
		}
		for _, repo := range def.Trigger.Forgejo.Repos {
			s.pollRepo(ctx, v, *def.Trigger.Forgejo, repo)
		}
	}
	return nil
}

func (s *Service) pollRepo(ctx context.Context, v process.WorkflowVersion, trig workflow.ForgejoTrigger, repoRef string) {
	repo, err := projects.SplitRepo(repoRef)
	if err != nil {
		slog.ErrorContext(ctx, "invalid trigger repo",
			slog.String("workflow", v.Name),
			slog.Any("error", err),
		)
		return
	}
	var assignee *string
	if trig.Assignee != "" {
		assignee = &trig.Assignee
	}
	issues, err := s.fj.GetIssues(repo.Owner, repo.Name, trig.Label, assignee)
	if err != nil {
		slog.WarnContext(ctx, "fetch issues failed",
			slog.String("workflow", v.Name),
			slog.String("repo", repoRef),
			slog.Any("error", err),
		)
		return
	}
	for _, issue := range issues {
		if slices.Contains(issue.Labels, forgejo.LabelDone) {
			continue
		}
		issue.RepoOwner = repo.Owner
		issue.RepoName = repo.Name
		project, ok := projects.ProjectForLabels(s.cfg.Projects, issue.Labels)
		if !ok {
			slog.WarnContext(ctx, "issue has no configured project label",
				slog.String("repo", repoRef),
				slog.Int("issue", issue.Number),
			)
			continue
		}
		res, err := s.start(ctx, v, issue, project)
		if err != nil {
			slog.ErrorContext(ctx, "start process failed",
				slog.String("workflow", v.Name),
				slog.String("repo", repoRef),
				slog.Int("issue", issue.Number),
				slog.Any("error", err),
			)
			continue
		}
		if res.Created {
			slog.InfoContext(ctx, "process started",
				slog.String("workflow", v.Name),
				slog.String("process_id", res.Process.ID),
				slog.String("subject", res.Process.Subject.Key),
				slog.String("project", project),
			)
		}
	}
}

func (s *Service) start(ctx context.Context, v process.WorkflowVersion, issue models.ForgejoIssue, project string) (engine.StartResult, error) {
	slug := issue.RepoOwner + "/" + issue.RepoName
	vars := process.Vars{}
	if err := process.Set(vars, steps.KeyIssue, issue); err != nil {
		return engine.StartResult{}, err
	}
	if err := process.Set(vars, steps.KeyProject, project); err != nil {
		return engine.StartResult{}, err
	}
	return s.eng.Start(ctx, engine.StartRequest{
		WorkflowVersionID: v.ID,
		Subject: process.Subject{
			Kind: process.SubjectIssue,
			Key:  slug + "#" + strconv.Itoa(issue.Number),
			URL:  strings.TrimRight(s.cfg.ForgejoURL, "/") + "/" + slug + "/issues/" + strconv.Itoa(issue.Number),
		},
		Title:   issue.Title,
		Project: project,
		Vars:    vars,
	})
}

func (s *Service) ResumeWaiting(ctx context.Context) error {
	waiting, err := s.store.ListWaiting(ctx, process.WaitComment)
	if err != nil {
		return fmt.Errorf("list waiting processes: %w", err)
	}
	for _, p := range waiting {
		s.resumeIfAnswered(ctx, p)
	}
	return nil
}

func (s *Service) resumeIfAnswered(ctx context.Context, p process.Process) {
	if p.WaitingFor == nil {
		return
	}
	issue, ok, err := process.Get(p.Vars, steps.KeyIssue)
	if err != nil || !ok {
		slog.ErrorContext(ctx, "waiting process has no usable issue var",
			slog.String("process_id", p.ID),
			slog.Bool("present", ok),
			slog.Any("error", err),
		)
		return
	}
	comments, err := s.fj.GetComments(issue.RepoOwner, issue.RepoName, issue.Number)
	if err != nil {
		slog.WarnContext(ctx, "fetch comments failed",
			slog.String("process_id", p.ID),
			slog.Any("error", err),
		)
		return
	}
	answered := slices.ContainsFunc(comments, func(c models.IssueComment) bool {
		return c.Author != s.cfg.BotUser && c.UpdatedAt.After(p.WaitingFor.Since)
	})
	if !answered {
		return
	}
	if err := s.eng.Resume(ctx, p.ID); err != nil {
		slog.ErrorContext(ctx, "resume process failed",
			slog.String("process_id", p.ID),
			slog.Any("error", err),
		)
		return
	}
	slog.InfoContext(ctx, "process resumed",
		slog.String("process_id", p.ID),
		slog.String("subject", p.Subject.Key),
	)
}

func (s *Service) StartForIssue(ctx context.Context, workflowName, subjectKey string) (process.Process, error) {
	repoRef, numStr, ok := strings.Cut(subjectKey, "#")
	repo, repoErr := projects.SplitRepo(repoRef)
	number, numErr := strconv.Atoi(numStr)
	if !ok || repoErr != nil || numErr != nil || number <= 0 {
		return process.Process{}, fmt.Errorf("%w: %q", ErrInvalidSubject, subjectKey)
	}
	versions, err := s.store.LatestWorkflowVersions(ctx)
	if err != nil {
		return process.Process{}, fmt.Errorf("latest workflow versions: %w", err)
	}
	idx := slices.IndexFunc(versions, func(v process.WorkflowVersion) bool { return v.Name == workflowName })
	if idx < 0 {
		return process.Process{}, fmt.Errorf("%w: %s", ErrUnknownWorkflow, workflowName)
	}
	issue, err := s.fj.GetIssue(repo.Owner, repo.Name, number)
	if err != nil {
		return process.Process{}, fmt.Errorf("%w: get issue %s: %w", step.ErrUpstream, subjectKey, err)
	}
	issue.RepoOwner = repo.Owner
	issue.RepoName = repo.Name
	project, found := projects.ProjectForLabels(s.cfg.Projects, issue.Labels)
	if !found {
		return process.Process{}, fmt.Errorf("%w: %s", ErrNoProject, subjectKey)
	}
	res, err := s.start(ctx, versions[idx], issue, project)
	if err != nil {
		return process.Process{}, fmt.Errorf("start process for %s: %w", subjectKey, err)
	}
	if !res.Created {
		return process.Process{}, fmt.Errorf("%w: %s", ErrProcessExists, subjectKey)
	}
	return res.Process, nil
}
