import google.generativeai as genai

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


class GeminiReviewer:
    def __init__(self, api_key: str) -> None:
        genai.configure(api_key=api_key)
        self._model = genai.GenerativeModel("gemini-2.0-flash")

    def review(self, job: ReviewJob) -> ReviewResult:
        prompt = REVIEW_PROMPT_TEMPLATE.format(
            pr_title=job.pr_title,
            issue_number=job.issue_number,
            diff=job.diff,
        )
        response = self._model.generate_content(prompt)
        return parse_review_text(response.text)
