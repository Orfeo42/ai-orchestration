from pathlib import Path

import google.generativeai as genai
import structlog

from shared.models import ReviewJob, ReviewResult

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
        genai.configure(api_key=api_key)
        self._model = genai.GenerativeModel("gemini-2.0-flash")

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

        response = self._model.generate_content(prompt)
        new_content = response.text
        if new_content.strip() == existing.strip():
            log.info("README remains unchanged")
            return False

        readme_path.write_text(new_content)
        log.info("README updated successfully")
        return True
