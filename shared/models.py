from pydantic import BaseModel


class ForgejoIssue(BaseModel):
    id: int
    number: int
    title: str
    body: str
    repo_owner: str
    repo_name: str
    labels: list[str]


class ForgejoPR(BaseModel):
    number: int
    title: str
    body: str = ""


class ReviewJob(BaseModel):
    repo_owner: str
    repo_name: str
    pr_number: int
    pr_title: str
    pr_branch: str
    project_path: str
    issue_number: int
    diff: str


class ReviewResult(BaseModel):
    summary: str
    issues: list[str]
    suggestions: list[str]
    approved: bool


def parse_review_text(text: str) -> ReviewResult:
    lines = text.strip().split("\n")
    summary = ""
    approved = False
    issues: list[str] = []
    suggestions: list[str] = []
    section: str | None = None

    for line in lines:
        if line.startswith("SUMMARY:"):
            summary = line.removeprefix("SUMMARY:").strip()
            section = None
        elif line.startswith("APPROVED:"):
            approved = "yes" in line.lower()
            section = None
        elif line.startswith("ISSUES:"):
            section = "issues"
        elif line.startswith("SUGGESTIONS:"):
            section = "suggestions"
        elif line.startswith("- "):
            item = line[2:]
            if section == "issues":
                issues.append(item)
            elif section == "suggestions":
                suggestions.append(item)

    return ReviewResult(summary=summary, approved=approved, issues=issues, suggestions=suggestions)
