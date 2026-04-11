import anthropic

from shared.models import ReviewJob, ReviewResult, parse_review_text

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
        prompt = REVIEW_PROMPT_TEMPLATE.format(
            pr_title=job.pr_title,
            issue_number=job.issue_number,
            diff=job.diff,
        )
        msg = self._client.messages.create(
            model="claude-sonnet-4-6",
            max_tokens=2048,
            messages=[{"role": "user", "content": prompt}],
        )
        return parse_review_text(msg.content[0].text)
