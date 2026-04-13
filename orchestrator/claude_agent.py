import json
import subprocess
import threading
import time
from typing import IO

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
        prompt = _build_prompt(issue, test_error)
        log.info("running claude agent", issue=issue.number, project_path=project_path, resume=resume)

        extra = ["--continue"] if resume else []
        stdout, stderr, returncode = self._run_streaming(extra, prompt, project_path)

        if returncode != 0:
            log.error("claude agent failed", code=returncode, stderr=stderr[:1000])
            raise RuntimeError(f"claude exited {returncode}\nstderr: {stderr}")

        log.info("claude agent finished successfully", issue=issue.number)
        return stdout

    def _run_streaming(
        self,
        extra_flags: list[str],
        prompt: str,
        cwd: str,
    ) -> tuple[str, str, int]:
        cmd = [
            "claude",
            "--print",
            "--dangerously-skip-permissions",
            "--output-format", "stream-json",
            "--verbose",
            "--allowedTools", "Bash,Read,Edit,Write,Glob,Grep",
            *extra_flags,
        ]

        proc = subprocess.Popen(
            cmd,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            cwd=cwd,
        )

        stdout_lines: list[str] = []
        stderr_lines: list[str] = []

        def stream_stdout(pipe: IO[str]) -> None:
            for line in pipe:
                stdout_lines.append(line)
                _log_event(line.strip())

        def stream_stderr(pipe: IO[str]) -> None:
            for line in pipe:
                stderr_lines.append(line)
                log.warning("claude stderr", output=line.rstrip())

        t_out = threading.Thread(target=stream_stdout, args=(proc.stdout,), daemon=True)
        t_err = threading.Thread(target=stream_stderr, args=(proc.stderr,), daemon=True)
        t_out.start()
        t_err.start()

        assert proc.stdin is not None
        proc.stdin.write(prompt)
        proc.stdin.close()

        deadline = time.monotonic() + self._timeout
        while proc.poll() is None:
            if time.monotonic() > deadline:
                proc.kill()
                raise TimeoutError(f"claude timed out after {self._timeout}s")
            time.sleep(1)

        t_out.join()
        t_err.join()

        return "".join(stdout_lines), "".join(stderr_lines), proc.returncode


def _log_event(line: str) -> None:
    if not line:
        return
    try:
        event = json.loads(line)
    except json.JSONDecodeError:
        log.info("claude", output=line)
        return

    event_type = event.get("type", "")
    if event_type == "assistant":
        for block in event.get("message", {}).get("content", []):
            if block.get("type") == "text":
                text = block.get("text", "").strip()
                if text:
                    log.info("claude text", output=text[:500])
            elif block.get("type") == "tool_use":
                log.info("claude tool", tool=block.get("name"), input=str(block.get("input", ""))[:200])
    elif event_type == "tool_result":
        content = event.get("content", "")
        if isinstance(content, list):
            for c in content:
                if c.get("type") == "text":
                    log.info("claude tool result", output=c.get("text", "")[:200])
        elif isinstance(content, str) and content:
            log.info("claude tool result", output=content[:200])


def _build_prompt(issue: ForgejoIssue, test_error: str | None) -> str:
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
