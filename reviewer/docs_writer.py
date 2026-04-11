from pathlib import Path

import anthropic

from shared.models import ReviewJob, ReviewResult

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

        prompt = DOCS_PROMPT_TEMPLATE.format(
            pr_title=job.pr_title,
            summary=review.summary,
            readme=existing,
            diff=job.diff[:4000],
        )

        msg = self._client.messages.create(
            model="claude-sonnet-4-6",
            max_tokens=4096,
            messages=[{"role": "user", "content": prompt}],
        )

        new_content = msg.content[0].text
        if new_content.strip() == existing.strip():
            return False

        readme_path.write_text(new_content)
        return True
