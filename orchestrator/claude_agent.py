import os
import subprocess

import structlog

from shared.models import ForgejoIssue

log = structlog.get_logger(__name__)


class ClaudeAgent:
    def __init__(self, api_key: str) -> None:
        self._api_key = api_key

    def implement_issue(
        self,
        issue: ForgejoIssue,
        project_path: str,
        test_error: str | None = None,
    ) -> None:
        prompt = self._build_prompt(issue, test_error)
        env = {
            **os.environ,
            "ANTHROPIC_API_KEY": self._api_key,
        }

        log.info("running claude agent", issue=issue.number, project_path=project_path)
        
        result = subprocess.run(
            [
                "claude",
                "--print",
                "--dangerously-skip-permissions",
                "--output-format", "text",
                "--allowedTools", "Bash,Read,Edit,Write,Glob,Grep",
            ],
            input=prompt,
            cwd=project_path,
            env=env,
            capture_output=True,
            text=True,
            timeout=600,
        )

        if result.returncode != 0:
            log.error("claude agent failed", code=result.returncode, stderr=result.stderr[:1000], stdout=result.stdout[:500])
            raise RuntimeError(f"claude exited {result.returncode}")
        
        log.info("claude agent finished successfully", issue=issue.number)

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
