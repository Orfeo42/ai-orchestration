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

Read project files as needed to understand context. Decide if the issue has enough detail to implement without guessing core behavior, public API, data model, or acceptance criteria.

Output exactly one JSON line, nothing else. No preamble, no code fences, no trailing text.
Schema:
  {"clear": true}
  {"clear": false, "questions": "- what is missing or ambiguous\n- another point"}

The "questions" field, when present, must be concise markdown bullet points addressed to the issue author.`

func (a *ClaudeAgent) AssessIssue(issue models.ForgejoIssue, projectPath string) (Assessment, error) {
	slog.Info("assessing issue clarity",
		"issue", issue.Number,
		"path", projectPath,
	)

	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()

	prompt := fmt.Sprintf(claudeAssessPrompt, issue.Number, issue.Title, issue.Body)

	out, err := runClaudeStream(ctx, projectPath, prompt, []string{
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Bash,Read,Glob,Grep",
	})
	if err != nil {
		return Assessment{}, fmt.Errorf("claude assess: %w", err)
	}
	if out == "" {
		return Assessment{}, errEmptyAssessment
	}
	return parseAssessment(out)
}

func runClaudeStream(ctx context.Context, projectPath, prompt string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectPath
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start claude: %w", err)
	}

	var result string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		logEvent(line)
		if r := resultFromEvent(line); r != "" {
			result = r
		}
	}

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("claude exited: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(result), nil
}

func resultFromEvent(line string) string {
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return ""
	}
	if event["type"] != "result" {
		return ""
	}
	r, _ := event["result"].(string)
	return r
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

func (a *ClaudeAgent) ImplementIssue(issue models.ForgejoIssue, projectPath string, testError *string, resume bool) (string, error) {
	prompt := buildPrompt(issue, testError)
	slog.Info("running claude agent",
		"issue", issue.Number,
		"path", projectPath,
		"resume", resume,
	)

	args := []string{
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Bash,Read,Edit,Write,Glob,Grep",
	}
	if resume {
		args = append(args, "--continue")
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = projectPath

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start claude: %w", err)
	}

	if _, err := fmt.Fprint(stdin, prompt); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}

	if err := stdin.Close(); err != nil {
		slog.Error("stdin close failed",
			"error", err,
		)
	}

	var outLines []string
	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			outLines = append(outLines, line)
			logEvent(line)
		}
	}()

	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			slog.Warn("claude stderr",
				"output", scanner.Text(),
			)
		}
	}()

	waitDone := make(chan error, 1)
	go func() {
		<-outDone
		<-errDone
		waitDone <- cmd.Wait()
	}()

	select {
	case err := <-waitDone:
		if err != nil {
			return "", fmt.Errorf("claude exited with error: %w", err)
		}
	case <-time.After(a.timeout):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("claude timed out after %s", a.timeout)
	}

	slog.Info("claude agent finished",
		"issue", issue.Number,
	)
	return strings.Join(outLines, "\n"), nil
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
