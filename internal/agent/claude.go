package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

var errEmptyAssessment = errors.New("empty assessment output")

type ClaudeAgent struct {
	timeout time.Duration
}

func New(timeout time.Duration) *ClaudeAgent {
	return &ClaudeAgent{timeout: timeout}
}

const claudeAssessPrompt = `You are triaging a Forgejo issue before implementation.

Issue #%d: %s

%s

Existing comments (chronological, may contain clarifications from the author or reviewers):
%s

Read project files as needed to understand context. Treat comments as authoritative clarifications that extend or refine the issue body. Decide if the issue — including any clarifications in comments — has enough detail to implement without guessing core behavior, public API, data model, or acceptance criteria.

Output exactly one JSON line, nothing else. No preamble, no code fences, no trailing text.
Schema:
  {"clear": true}
  {"clear": false, "questions": "- what is missing or ambiguous\n- another point"}

The "questions" field, when present, must be concise markdown bullet points addressed to the issue author. Do not repeat questions already answered in the comments.`

func formatComments(comments []models.IssueComment) string {
	if len(comments) == 0 {
		return "(no comments)"
	}
	var b strings.Builder
	for i, c := range comments {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		author := c.Author
		if author == "" {
			author = "unknown"
		}
		fmt.Fprintf(&b, "@%s:\n%s", author, c.Body)
	}
	return b.String()
}

func (a *ClaudeAgent) AssessIssue(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (AssessResult, error) {
	slog.InfoContext(ctx, "assessing issue clarity",
		"issue", issue.Number,
		"path", projectPath,
		"comments", len(comments),
	)

	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	prompt := fmt.Sprintf(claudeAssessPrompt, issue.Number, issue.Title, issue.Body, formatComments(comments))

	out, err := runClaudeStream(cctx, projectPath, prompt, []string{
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Bash,Read,Glob,Grep",
	}, sink)
	if err != nil {
		return AssessResult{}, fmt.Errorf("claude assess: %w", err)
	}
	if out.Result == "" {
		return AssessResult{}, errEmptyAssessment
	}
	assess, err := parseAssessment(out.Result)
	if err != nil {
		return AssessResult{}, err
	}
	return AssessResult{Assessment: assess, SessionID: out.SessionID, Usage: out.Usage}, nil
}

type streamOutcome struct {
	Result    string
	SessionID string
	Usage     events.TokenUsage
	Output    string
}

func runClaudeStream(ctx context.Context, projectPath, prompt string, args []string, sink events.Sink) (streamOutcome, error) {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectPath
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return streamOutcome{}, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Start(); err != nil {
		return streamOutcome{}, fmt.Errorf("start claude: %w", err)
	}

	outcome, err := consumeStream(ctx, stdout, sink)
	if werr := cmd.Wait(); werr != nil {
		if err == nil {
			err = fmt.Errorf("claude exited: %w (stderr: %s)", werr, strings.TrimSpace(stderr.String()))
		}
	}
	return outcome, err
}

func consumeStream(ctx context.Context, stdout io.Reader, sink events.Sink) (streamOutcome, error) {
	if sink == nil {
		sink = events.NoopSink{}
	}
	var (
		outcome streamOutcome
		buf     strings.Builder
	)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line)
		buf.WriteString("\n")
		logEvent(line)
		for _, evt := range events.ParseClaudeLine(line) {
			updateOutcome(&outcome, evt)
			if err := sink.Emit(ctx, evt); err != nil {
				slog.WarnContext(ctx, "sink emit failed",
					"error", err,
				)
			}
		}
	}
	outcome.Output = buf.String()
	if err := scanner.Err(); err != nil {
		return outcome, fmt.Errorf("scan stdout: %w", err)
	}
	return outcome, nil
}

func updateOutcome(o *streamOutcome, evt events.Event) {
	if evt.SessionID != "" {
		o.SessionID = evt.SessionID
	}
	if evt.Usage != nil {
		o.Usage = o.Usage.Add(*evt.Usage)
	}
	if evt.Kind == events.KindResult && evt.ResultText != "" {
		o.Result = evt.ResultText
	}
}

func parseAssessment(out string) (Assessment, error) {
	line := lastJSONLine(out)
	if line == "" {
		return Assessment{}, fmt.Errorf("assessment: no json line in output: %s", out[:min(200, len(out))])
	}
	var raw struct {
		Clear     bool   `json:"clear"`
		Questions string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Assessment{}, fmt.Errorf("assessment: unmarshal %q: %w", line, err)
	}
	if !raw.Clear && strings.TrimSpace(raw.Questions) == "" {
		return Assessment{}, fmt.Errorf("assessment: clear=false but questions missing")
	}
	return Assessment{Clear: raw.Clear, Questions: raw.Questions}, nil
}

func lastJSONLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			return trimmed
		}
	}
	return ""
}

func (a *ClaudeAgent) ImplementIssue(ctx context.Context, issue models.ForgejoIssue, projectPath, sessionID string, testError *string, sink events.Sink) (ImplementResult, error) {
	prompt := buildPrompt(issue, testError)
	slog.InfoContext(ctx, "running claude agent",
		"issue", issue.Number,
		"path", projectPath,
		"session", sessionID,
	)

	args := []string{
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Bash,Read,Edit,Write,Glob,Grep",
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	out, err := runClaudeStream(cctx, projectPath, prompt, args, sink)
	if err != nil {
		return ImplementResult{}, fmt.Errorf("claude impl: %w", err)
	}

	finalSession := out.SessionID
	if finalSession == "" {
		finalSession = sessionID
	}
	slog.InfoContext(ctx, "claude agent finished",
		"issue", issue.Number,
		"session", finalSession,
	)
	return ImplementResult{Output: out.Output, SessionID: finalSession, Usage: out.Usage}, nil
}

func logEvent(line string) {
	if line == "" {
		return
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		slog.Info("claude",
			"output", line,
		)
		return
	}
	switch event["type"] {
	case "assistant":
		msg, _ := event["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		for _, block := range content {
			b, _ := block.(map[string]any)
			switch b["type"] {
			case "text":
				text, _ := b["text"].(string)
				if text != "" {
					slog.Info("claude text",
						"output", text[:min(500, len(text))],
					)
				}
			case "tool_use":
				input := fmt.Sprintf("%v", b["input"])
				slog.Info("claude tool",
					"tool", b["name"],
					"input", input[:min(200, len(input))],
				)
			}
		}
	case "tool_result":
		switch v := event["content"].(type) {
		case string:
			if v != "" {
				slog.Info("claude tool result",
					"output", v[:min(200, len(v))],
				)
			}
		case []any:
			for _, c := range v {
				m, _ := c.(map[string]any)
				if m["type"] == "text" {
					text, _ := m["text"].(string)
					slog.Info("claude tool result",
						"output", text[:min(200, len(text))],
					)
				}
			}
		}
	}
}

func buildPrompt(issue models.ForgejoIssue, testError *string) string {
	base := fmt.Sprintf(`Implement the following issue for this project.

Issue #%d: %s

%s

Requirements:
- Implement only what the issue describes
- Follow existing code style and conventions in the project
- Do not add comments, docstrings, or documentation inline
- Only modify files relevant to the issue`, issue.Number, issue.Title, issue.Body)

	if testError != nil {
		base += fmt.Sprintf(`

A previous implementation attempt failed the test suite. Fix the failing tests.
Test output:
%s`, *testError)
	}
	return base
}
