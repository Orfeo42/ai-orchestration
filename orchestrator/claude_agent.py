import subprocess

import structlog

from shared.models import ForgejoIssue

log = structlog.get_logger(__name__)


class ClaudeAgent:
    def __init__(self, timeout: int = 3600) -> None:
        self._timeout = timeout

    def implement_issue(
        self,
        issue: ForgejoIssue,
        project_path: str,
        test_error: str | None = None,
        resume: bool = False,
    ) -> str:
        prompt = self._build_prompt(issue, test_error)
        log.info("running claude agent", issue=issue.number, project_path=project_path, resume=resume)

        extra = ["--continue"] if resume else []
        result = self._run(extra, prompt, project_path)

        output = f"--- STDOUT ---\n{result.stdout}\n--- STDERR ---\n{result.stderr}"
        if result.returncode != 0:
            log.error("claude agent failed", code=result.returncode, stderr=result.stderr[:1000], stdout=result.stdout[:500])
            raise RuntimeError(f"claude exited {result.returncode}\n\n{output}")

        log.info("claude agent finished successfully", issue=issue.number)
        return output

    def _run(self, extra_flags: list[str], prompt: str, cwd: str) -> subprocess.CompletedProcess[str]:
        cmd = [
            "claude",
            "--print",
            "--dangerously-skip-permissions",
            "--output-format", "text",
            "--allowedTools", "Bash,Read,Edit,Write,Glob,Grep",
            *extra_flags,
        ]
        return subprocess.run(
            cmd,
            input=prompt,
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=self._timeout,
        )

    def _build_prompt(self, issue: ForgejoIssue, test_error: str | None) -> str:
        base = f"""Implement the following issue for this project.

Issue #{issue.number}: {issue.title}

{issue.body}

Requirements:
- Implement only what the issue describes
- Follow existing code style and conventions in the project
- Do not add comments, docstrings, or documentation inline
- Only modify files relevant to the issue"""

        if test_error:
            base += f"""

A previous implementation attempt failed the test suite. Fix the failing tests.
Test output:
{test_error}"""

        return base
