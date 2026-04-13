import anthropic
import structlog

from shared.models import ReviewJob, ReviewResult, parse_review_text

from shared.constants import CLAUDE_MODEL

log = structlog.get_logger(__name__)

REVIEW_PROMPT_TEMPLATE = """Review this pull request diff. Be concise and direct.

PR: {pr_title}
Issue: #{issue_number}

Diff:
```diff
{diff}
```

Respond in this exact format:
SUMMARY: <one paragraph>
APPROVED: <yes or no>
ISSUES:
- <blocking issue, omit section if none>
SUGGESTIONS:
- <non-blocking suggestion, omit section if none>"""


class ClaudeReviewer:
    def __init__(self, api_key: str) -> None:
        self._client = anthropic.Anthropic(api_key=api_key)

    def review(self, job: ReviewJob) -> ReviewResult:
        log.info("requesting claude review", pr_number=job.pr_number)
        prompt = REVIEW_PROMPT_TEMPLATE.format(
            pr_title=job.pr_title,
            issue_number=job.issue_number,
            diff=job.diff,
        )
        msg = self._client.messages.create(
            model=CLAUDE_MODEL,
            max_tokens=2048,
            messages=[{"role": "user", "content": prompt}],
        )
        text_block = next(b for b in msg.content if b.type == "text")
        text = text_block.text
        log.debug("claude review received", text=text)
        return parse_review_text(text)
