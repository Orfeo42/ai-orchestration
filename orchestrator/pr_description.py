import google.generativeai as genai
import structlog
from shared.models import ForgejoIssue

log = structlog.get_logger(__name__)

_PROMPT = """Write a pull request description for the following implementation.

Issue #{issue_number}: {issue_title}

{issue_body}

Diff:
```diff
{diff}
```

Write a concise PR description in markdown. Include:
- What was implemented
- Key technical decisions if non-obvious
- No fluff, no summaries of the diff line by line"""


class PRDescriptionWriter:
    def __init__(self, api_key: str) -> None:
        genai.configure(api_key=api_key)
        self._model = genai.GenerativeModel("gemini-2.0-flash")

    def generate(self, issue: ForgejoIssue, diff: str) -> str:
        log.info("generating PR description", issue=issue.number)
        prompt = _PROMPT.format(
            issue_number=issue.number,
            issue_title=issue.title,
            issue_body=issue.body or "",
            diff=diff[:50000],
        )
        response = self._model.generate_content(prompt)
        return response.text.strip()
