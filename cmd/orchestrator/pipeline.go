package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/control"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/testrunner"
)

var (
	errNoChanges    = errors.New("no changes produced")
	errIssueUnclear = errors.New("issue requirements unclear")
)

type prDescWriter interface {
	Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error)
}

type fallbackPRWriter struct {
	primary  prDescWriter
	fallback prDescWriter
}

func (f *fallbackPRWriter) Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
	desc, err := f.primary.Generate(ctx, issue, projectPath, diff, sink)
	if err == nil {
		return desc, nil
	}
	if f.fallback == nil {
		return "", err
	}
	slog.WarnContext(ctx, "primary pr writer failed, using fallback",
		"error", err,
	)
	return f.fallback.Generate(ctx, issue, projectPath, diff, sink)
}

type orchDeps struct {
	fj         forgejo.Forgejo
	rq         queue.Queue
	claude     agent.Agent
	prWriter   prDescWriter
	gitFactory gitops.Factory
	runTests   testrunner.Runner
	store      runstore.Store
	ctrl       control.Channel
}

func pollRepo(ctx context.Context, owner, repo string, deps orchDeps, cfg config) {
	issues, err := deps.fj.GetIssues(owner, repo, forgejo.LabelImplement, cfg.forgejoAssignee)
	if err != nil {
		slog.ErrorContext(ctx, "fetch issues failed",
			"repo", owner+"/"+repo,
			"error", err,
		)
		return
	}

	for _, issue := range issues {
		key := fmt.Sprintf("%s/%s#%d", owner, repo, issue.Number)

		processed, err := deps.rq.IsProcessed(ctx, key)
		if err != nil {
			slog.ErrorContext(ctx, "check processed failed",
				"key", key,
				"error", err,
			)
			continue
		}
		if processed || containsLabel(issue.Labels, forgejo.LabelDone) {
			continue
		}
		if containsLabel(issue.Labels, forgejo.LabelNeedsInfo) {
			slog.InfoContext(ctx, "issue waiting on author, skipping",
				"issue", issue.Number,
			)
			continue
		}

		projectName := projectLabel(issue.Labels)
		if projectName == "" {
			slog.WarnContext(ctx, "issue has no project label, skipping",
				"issue", issue.Number,
			)
			continue
		}
		proj, ok := cfg.projects[projectName]
		if !ok {
			slog.ErrorContext(ctx, "unknown project",
				"project", projectName,
				"issue", issue.Number,
			)
			continue
		}

		if err := deps.rq.MarkInProgress(ctx, key, 2*time.Hour); err != nil {
			slog.ErrorContext(ctx, "mark in-progress failed",
				"key", key,
				"error", err,
			)
			continue
		}

		startIssue(ctx, deps.fj, owner, repo, issue)

		slog.InfoContext(ctx, "processing issue",
			"issue", issue.Number,
			"project", proj.Repo,
		)
		procErr := processIssue(ctx, issue, proj, owner, repo, deps, cfg)

		if clearErr := deps.rq.ClearInProgress(ctx, key); clearErr != nil {
			slog.WarnContext(ctx, "clear in-progress failed",
				"key", key,
				"error", clearErr,
			)
		}

		if procErr != nil {
			if errors.Is(procErr, errIssueUnclear) {
				continue
			}
			slog.ErrorContext(ctx, "issue processing failed",
				"issue", issue.Number,
				"error", procErr,
			)
			if labelID, lerr := deps.fj.GetLabelID(owner, repo, forgejo.LabelInProgress); lerr == nil && labelID != nil {
				if rerr := deps.fj.RemoveLabel(owner, repo, issue.Number, *labelID); rerr != nil {
					slog.WarnContext(ctx, "remove in-progress label failed",
						"error", rerr,
					)
				}
			}
			continue
		}

		if err := deps.rq.MarkProcessed(ctx, key); err != nil {
			slog.WarnContext(ctx, "mark processed failed",
				"key", key,
				"error", err,
			)
		}
	}
}

func startIssue(ctx context.Context, fj forgejo.Forgejo, owner, repo string, issue models.ForgejoIssue) {
	if labelID, err := fj.GetLabelID(owner, repo, forgejo.LabelInProgress); err == nil && labelID != nil {
		if err := fj.AddLabel(owner, repo, issue.Number, *labelID); err != nil {
			slog.WarnContext(ctx, "add in-progress label failed",
				"error", err,
			)
		}
	}
}

type runHandle struct {
	id           string
	sink         events.Sink
	store        runstore.Store
	notesMu      sync.Mutex
	notes        []string
	ctx          context.Context
	cancel       context.CancelFunc
	seenComments map[int64]time.Time
}

func (h *runHandle) markCommentsSeen(comments []models.IssueComment) {
	for _, c := range comments {
		h.seenComments[c.ID] = c.UpdatedAt
	}
}

func (h *runHandle) freshComments(comments []models.IssueComment) []models.IssueComment {
	var fresh []models.IssueComment
	for _, c := range comments {
		prev, ok := h.seenComments[c.ID]
		if !ok || c.UpdatedAt.After(prev) {
			fresh = append(fresh, c)
		}
		h.seenComments[c.ID] = c.UpdatedAt
	}
	return fresh
}

func (h *runHandle) drainNotes() []string {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	notes := h.notes
	h.notes = nil
	return notes
}

func (h *runHandle) appendNote(note string) {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	h.notes = append(h.notes, note)
}

func (h *runHandle) emit(ctx context.Context, evt events.Event) {
	if h.sink == nil {
		return
	}
	if err := h.sink.Emit(ctx, evt); err != nil {
		slog.WarnContext(ctx, "sink emit failed",
			"error", err,
		)
	}
}

func (h *runHandle) setPhase(ctx context.Context, phase runstore.Phase) {
	if h.store != nil {
		if err := h.store.Update(ctx, h.id, func(r *runstore.Run) {
			r.Phase = phase
		}); err != nil {
			slog.WarnContext(ctx, "update phase failed",
				"run", h.id,
				"error", err,
			)
		}
	}
	h.emit(ctx, events.Event{Kind: events.KindPhase, Phase: string(phase)})
}

func (h *runHandle) addUsage(ctx context.Context, u events.TokenUsage) {
	if h.store == nil {
		return
	}
	if err := h.store.Update(ctx, h.id, func(r *runstore.Run) {
		r.Usage = r.Usage.Add(u)
	}); err != nil {
		slog.WarnContext(ctx, "update usage failed",
			"run", h.id,
			"error", err,
		)
	}
}

func (h *runHandle) setSession(ctx context.Context, sessionID string) {
	if h.store == nil || sessionID == "" {
		return
	}
	if err := h.store.Update(ctx, h.id, func(r *runstore.Run) {
		r.ClaudeSession = sessionID
	}); err != nil {
		slog.WarnContext(ctx, "update session failed",
			"run", h.id,
			"error", err,
		)
	}
}

func (h *runHandle) setAttempt(ctx context.Context, attempt int) {
	if h.store == nil {
		return
	}
	if err := h.store.Update(ctx, h.id, func(r *runstore.Run) {
		r.Attempt = attempt
	}); err != nil {
		slog.WarnContext(ctx, "update attempt failed",
			"run", h.id,
			"error", err,
		)
	}
}

func startRunHandle(ctx context.Context, deps orchDeps, issue models.ForgejoIssue, proj projectConfig, owner, repo string) *runHandle {
	runID := uuid.NewString()
	h := &runHandle{id: runID, sink: events.NoopSink{}, seenComments: map[int64]time.Time{}}

	if deps.store != nil {
		run := runstore.Run{
			ID:          runID,
			Service:     runstore.ServiceOrchestrator,
			RepoOwner:   owner,
			RepoName:    repo,
			IssueNumber: issue.Number,
			Title:       issue.Title,
			Project:     proj.Repo,
			Phase:       runstore.PhaseAssessing,
			StartedAt:   time.Now().UTC(),
		}
		if err := deps.store.Create(ctx, run); err != nil {
			slog.WarnContext(ctx, "create run failed",
				"error", err,
			)
		} else {
			h.store = deps.store
			h.sink = deps.store.AppendSink(runID)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	h.ctx = runCtx
	h.cancel = cancel

	if deps.ctrl != nil {
		sub, err := deps.ctrl.Subscribe(runCtx, runID)
		if err != nil {
			slog.WarnContext(ctx, "ctrl subscribe failed",
				"run", runID,
				"error", err,
			)
		} else {
			go h.handleControl(runCtx, sub)
		}
	}

	h.emit(runCtx, events.Event{
		Kind:    events.KindRunStart,
		Service: string(runstore.ServiceOrchestrator),
	})

	return h
}

func (h *runHandle) handleControl(ctx context.Context, sub control.Subscription) {
	defer func() {
		if err := sub.Close(); err != nil {
			slog.WarnContext(ctx, "ctrl subscription close failed",
				"run", h.id,
				"error", err,
			)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.Messages():
			if !ok {
				return
			}
			switch msg.Type {
			case control.TypeCancel:
				h.emit(ctx, events.Event{Kind: events.KindCancel})
				h.cancel()
				return
			case control.TypeInject:
				h.appendNote(msg.Note)
				h.emit(ctx, events.Event{Kind: events.KindInject, Text: msg.Note})
				h.cancel()
				return
			}
		}
	}
}

func (h *runHandle) finalize(ctx context.Context, phase runstore.Phase, finalErr error) {
	if h.store == nil {
		return
	}
	errStr := ""
	if finalErr != nil {
		errStr = finalErr.Error()
	}
	if err := h.store.Finalize(ctx, h.id, phase, errStr); err != nil {
		slog.WarnContext(ctx, "finalize run failed",
			"run", h.id,
			"error", err,
		)
	}
	h.emit(ctx, events.Event{Kind: events.KindRunEnd, Phase: string(phase)})
}

func processIssue(
	ctx context.Context,
	issue models.ForgejoIssue,
	proj projectConfig,
	issueOwner, issueRepo string,
	deps orchDeps,
	cfg config,
) (resErr error) {
	code, err := splitRepo(proj.Repo)
	if err != nil {
		return err
	}
	codeOwner, codeName := code.Owner, code.Name

	handle := startRunHandle(ctx, deps, issue, proj, issueOwner, issueRepo)
	runCtx := handle.ctx
	defer handle.cancel()
	defer func() {
		switch {
		case errors.Is(resErr, errIssueUnclear):
			handle.finalize(ctx, runstore.PhaseDone, nil)
		case resErr != nil:
			handle.finalize(ctx, runstore.PhaseFailed, resErr)
		default:
			handle.finalize(ctx, runstore.PhaseDone, nil)
		}
	}()

	comments, err := deps.fj.GetComments(issueOwner, issueRepo, issue.Number)
	if err != nil {
		slog.WarnContext(ctx, "fetch comments failed, assessing without them",
			"issue", issue.Number,
			"error", err,
		)
		comments = nil
	}

	handle.setPhase(runCtx, runstore.PhaseAssessing)
	assess, err := deps.claude.AssessIssue(runCtx, issue, comments, proj.path(), handle.sink)
	if err != nil {
		return fmt.Errorf("assess issue: %w", err)
	}
	handle.addUsage(runCtx, assess.Usage)
	handle.setSession(runCtx, assess.SessionID)
	handle.markCommentsSeen(comments)

	if !assess.Assessment.Clear {
		err := handleUnclearIssue(ctx, issue, issueOwner, issueRepo, assess.Assessment.Questions, deps)
		return err
	}
	sessionID := assess.SessionID

	branch := fmt.Sprintf("feature/%d-%s", issue.Number, branchSlug(issue.Title))
	var testError *string

	for attempt := range maxAttempts {
		handle.setAttempt(runCtx, attempt+1)
		git := deps.gitFactory(proj.path(), cfg.forgejoURL, cfg.forgejoToken, codeOwner, codeName)

		if attempt == 0 {
			if err := git.Checkout(proj.BaseBranch); err != nil {
				return fmt.Errorf("checkout base: %w", err)
			}
			if err := git.CreateBranch(branch); err != nil {
				return fmt.Errorf("create branch: %w", err)
			}
		} else {
			slog.InfoContext(ctx, "retry impl",
				"attempt", attempt+1,
				"issue", issue.Number,
			)
		}

		handle.setPhase(runCtx, runstore.PhaseImplementing)
		notes := handle.drainNotes()
		var freshComments []models.IssueComment
		if latest, cerr := deps.fj.GetComments(issueOwner, issueRepo, issue.Number); cerr != nil {
			slog.WarnContext(ctx, "fetch comments for impl failed, skipping delta",
				"issue", issue.Number,
				"error", cerr,
			)
		} else {
			freshComments = handle.freshComments(latest)
		}
		impl, err := deps.claude.ImplementIssue(runCtx, issue, freshComments, proj.path(), sessionID, withNotes(testError, notes), handle.sink)
		if err != nil {
			if pending := handle.drainNotes(); len(pending) > 0 || runCtx.Err() != nil {
				handle.notesMu.Lock()
				handle.notes = append(pending, handle.notes...)
				handle.notesMu.Unlock()
				if impl.SessionID != "" {
					sessionID = impl.SessionID
					handle.setSession(runCtx, sessionID)
				}
				handle.addUsage(runCtx, impl.Usage)
				if ctx.Err() != nil {
					handle.finalize(ctx, runstore.PhaseCancelled, nil)
					return nil
				}
				slog.InfoContext(ctx, "claude interrupted, resuming with injected notes",
					"issue", issue.Number,
				)
				attempt--
				continue
			}
			return fmt.Errorf("claude impl: %w", err)
		}
		handle.addUsage(runCtx, impl.Usage)
		if impl.SessionID != "" {
			sessionID = impl.SessionID
			handle.setSession(runCtx, sessionID)
		}

		changed, err := git.HasChanges()
		if err != nil {
			return fmt.Errorf("check changes: %w", err)
		}
		if !changed {
			slog.WarnContext(ctx, "no changes after impl",
				"issue", issue.Number,
			)
			return errNoChanges
		}

		handle.setPhase(runCtx, runstore.PhaseTesting)
		ok, tErr := deps.runTests(proj.path())
		if ok {
			break
		}

		truncated := tErr
		if len(truncated) > 200 {
			truncated = truncated[:200]
		}
		slog.WarnContext(ctx, "tests failed",
			"attempt", attempt+1,
			"issue", issue.Number,
			"error", truncated,
		)
		testError = &tErr

		if attempt+1 >= maxAttempts {
			return fmt.Errorf("max impl attempts reached, last test error: %s", tErr)
		}
	}

	handle.setPhase(runCtx, runstore.PhasePushing)
	if err := createPRAndQueue(runCtx, issue, proj, branch, issueOwner, issueRepo, codeOwner, codeName, deps, cfg, handle); err != nil {
		return err
	}
	return nil
}

func withNotes(testError *string, notes []string) *string {
	if len(notes) == 0 {
		return testError
	}
	combined := "Additional instructions from user:\n" + strings.Join(notes, "\n---\n")
	if testError != nil {
		combined += "\n\n" + *testError
	}
	return &combined
}

func handleUnclearIssue(
	ctx context.Context,
	issue models.ForgejoIssue,
	issueOwner, issueRepo, questions string,
	deps orchDeps,
) error {
	slog.InfoContext(ctx, "issue requirements unclear, requesting clarification",
		"issue", issue.Number,
	)

	body := "Requirements unclear, need clarification before implementation:\n\n" + questions
	if err := deps.fj.CreateComment(issueOwner, issueRepo, issue.Number, body); err != nil {
		return fmt.Errorf("create clarification comment: %w", err)
	}

	if labelID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelNeedsInfo); err == nil && labelID != nil {
		if err := deps.fj.AddLabel(issueOwner, issueRepo, issue.Number, *labelID); err != nil {
			slog.WarnContext(ctx, "add needs-info label failed",
				"error", err,
			)
		}
	} else if err != nil {
		slog.WarnContext(ctx, "lookup needs-info label failed",
			"error", err,
		)
	}

	if labelID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelInProgress); err == nil && labelID != nil {
		if err := deps.fj.RemoveLabel(issueOwner, issueRepo, issue.Number, *labelID); err != nil {
			slog.WarnContext(ctx, "remove in-progress label failed",
				"error", err,
			)
		}
	}

	return errIssueUnclear
}

func createPRAndQueue(
	ctx context.Context,
	issue models.ForgejoIssue,
	proj projectConfig,
	branch, issueOwner, issueRepo, codeOwner, codeName string,
	deps orchDeps,
	cfg config,
	handle *runHandle,
) error {
	git := deps.gitFactory(proj.path(), cfg.forgejoURL, cfg.forgejoToken, codeOwner, codeName)

	if err := git.AddAll(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := git.Commit(fmt.Sprintf("feat: implement issue #%d - %s", issue.Number, issue.Title)); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := git.Push(branch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	prBody := fmt.Sprintf("Implements [%s/%s#%d](%s/%s/%s/issues/%d)",
		issueOwner, issueRepo, issue.Number,
		cfg.forgejoURL, issueOwner, issueRepo, issue.Number)

	pr, err := deps.fj.CreatePR(codeOwner, codeName, "feat: "+issue.Title, prBody, branch, proj.BaseBranch)
	if err != nil {
		return fmt.Errorf("create pr: %w", err)
	}
	if handle != nil && handle.store != nil {
		if uerr := handle.store.Update(ctx, handle.id, func(r *runstore.Run) { r.PRNumber = pr.Number }); uerr != nil {
			slog.WarnContext(ctx, "update run pr number failed",
				"run", handle.id,
				"error", uerr,
			)
		}
	}

	diff, err := deps.fj.GetPRDiff(codeOwner, codeName, pr.Number)
	if err != nil {
		slog.WarnContext(ctx, "get pr diff failed",
			"error", err,
		)
	}

	if deps.prWriter != nil && diff != "" {
		if handle != nil {
			handle.setPhase(ctx, runstore.PhasePRDesc)
		}
		sink := events.Sink(events.NoopSink{})
		if handle != nil && handle.sink != nil {
			sink = handle.sink
		}
		desc, err := deps.prWriter.Generate(ctx, issue, proj.path(), diff, sink)
		if err != nil {
			slog.WarnContext(ctx, "pr description generation failed",
				"error", err,
			)
		} else {
			body := fmt.Sprintf("relates to: %s/%s#%d\n\n%s", issueOwner, issueRepo, issue.Number, desc)
			if err := deps.fj.UpdatePR(codeOwner, codeName, pr.Number, body); err != nil {
				slog.WarnContext(ctx, "update pr body failed",
					"error", err,
				)
			}
		}
	}

	if doneID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelDone); err == nil && doneID != nil {
		if err := deps.fj.AddLabel(issueOwner, issueRepo, issue.Number, *doneID); err != nil {
			slog.WarnContext(ctx, "add done label failed",
				"error", err,
			)
		}
	}
	if ipID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelInProgress); err == nil && ipID != nil {
		if err := deps.fj.RemoveLabel(issueOwner, issueRepo, issue.Number, *ipID); err != nil {
			slog.WarnContext(ctx, "remove in-progress label failed",
				"error", err,
			)
		}
	}

	job := models.ReviewJob{
		RepoOwner:   codeOwner,
		RepoName:    codeName,
		PRNumber:    pr.Number,
		PRTitle:     "feat: " + issue.Title,
		PRBranch:    branch,
		ProjectPath: proj.path(),
		IssueNumber: issue.Number,
		Diff:        diff,
	}
	if err := deps.rq.Push(ctx, "queue:review", job); err != nil {
		return fmt.Errorf("push review job: %w", err)
	}

	if err := git.Checkout(proj.BaseBranch); err != nil {
		slog.WarnContext(ctx, "checkout base after PR failed",
			"error", err,
		)
	}

	slog.InfoContext(ctx, "issue queued for review",
		"issue", issue.Number,
		"pr", fmt.Sprintf("%s/%s#%d", codeOwner, codeName, pr.Number),
	)
	return nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func branchSlug(title string) string {
	slug := slugRe.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
	}
	return slug
}

func containsLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func projectLabel(labels []string) string {
	for _, l := range labels {
		if after, ok := strings.CutPrefix(l, forgejo.LabelProjectPrefix); ok {
			return after
		}
	}
	return ""
}

type repoRef struct {
	Owner string
	Name  string
}

func splitRepo(repo string) (repoRef, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return repoRef{}, fmt.Errorf("invalid repo %q", repo)
	}
	return repoRef{Owner: parts[0], Name: parts[1]}, nil
}
