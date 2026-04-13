from pathlib import Path

import anthropic
import structlog

from shared.models import ReviewJob, ReviewResult

from shared.constants import CLAUDE_MODEL

log = structlog.get_logger(__name__)

DOCS_PROMPT_TEMPLATE = """Update the README.md for this project based on the changes in a merged PR.

PR: {pr_title}
Summary of changes: {summary}

Current README:
{readme}

Diff of changes (truncated):
```diff
{diff}
```

Return ONLY the updated README content, no preamble.
If no meaningful update is needed, return the original content unchanged."""


class DocsWriter:
    def __init__(self, api_key: str) -> None:
        self._client = anthropic.Anthropic(api_key=api_key)

    def update_readme(self, job: ReviewJob, review: ReviewResult) -> bool:
        readme_path = Path(job.project_path) / "README.md"
        existing = readme_path.read_text() if readme_path.exists() else ""

        log.info("updating README", project_path=job.project_path)
        prompt = DOCS_PROMPT_TEMPLATE.format(
            pr_title=job.pr_title,
            summary=review.summary,
            readme=existing,
            diff=job.diff[:4000],
        )

        msg = self._client.messages.create(
            model=CLAUDE_MODEL,
            max_tokens=4096,
            messages=[{"role": "user", "content": prompt}],
        )

        text_block = next(b for b in msg.content if b.type == "text")
        new_content = text_block.text
        if new_content.strip() == existing.strip():
            log.info("README remains unchanged")
            return False

        readme_path.write_text(new_content)
        log.info("README updated successfully")
        return True
