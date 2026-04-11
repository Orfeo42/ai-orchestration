import logging
from typing import TypedDict

from langgraph.graph import END, StateGraph

from claude_reviewer import ClaudeReviewer
from docs_writer import DocsWriter
from gemini_reviewer import GeminiReviewer
from shared.forgejo import ForgejoClient
from shared.git_ops import GitOps
from shared.models import ReviewJob, ReviewResult

log = logging.getLogger(__name__)


class ReviewerState(TypedDict):
    job: dict
    result: dict | None
    docs_updated: bool


def build_graph(
    reviewer: ClaudeReviewer | GeminiReviewer,
    docs_writer: DocsWriter,
    forgejo: ForgejoClient,
    forgejo_url: str,
    forgejo_token: str,
) -> object:
    def review(state: ReviewerState) -> dict:
        job = ReviewJob.model_validate(state["job"])
        result = reviewer.review(job)
        log.info("reviewed PR #%d (approved=%s)", job.pr_number, result.approved)
        return {"result": result.model_dump()}

    def update_docs(state: ReviewerState) -> dict:
        job = ReviewJob.model_validate(state["job"])
        result = ReviewResult.model_validate(state["result"])

        git = GitOps(job.project_path, forgejo_url, forgejo_token, job.repo_owner, job.repo_name)
        git.fetch()
        git.checkout_remote_branch(job.pr_branch)

        updated = docs_writer.update_readme(job, result)
        if updated:
            git.add("README.md")
            git.commit(f"docs: update README for PR #{job.pr_number}")
            git.push(job.pr_branch)
            log.info("README updated for PR #%d", job.pr_number)

        return {"docs_updated": updated}

    def post_review(state: ReviewerState) -> dict:
        job = ReviewJob.model_validate(state["job"])
        result = ReviewResult.model_validate(state["result"])

        lines = [f"## AI Review\n\n{result.summary}"]
        if result.issues:
            lines.append("\n### Issues\n" + "\n".join(f"- {i}" for i in result.issues))
        if result.suggestions:
            lines.append("\n### Suggestions\n" + "\n".join(f"- {s}" for s in result.suggestions))
        verdict = "APPROVED" if result.approved else "CHANGES REQUESTED"
        lines.append(f"\n**Verdict:** {verdict}")

        forgejo.create_pr_review(
            job.repo_owner,
            job.repo_name,
            job.pr_number,
            "\n".join(lines),
            result.approved,
        )
        return {}

    g = StateGraph(ReviewerState)
    g.add_node("review", review)
    g.add_node("update_docs", update_docs)
    g.add_node("post_review", post_review)

    g.set_entry_point("review")
    g.add_edge("review", "update_docs")
    g.add_edge("update_docs", "post_review")
    g.add_edge("post_review", END)

    return g.compile()


def initial_state(job: ReviewJob) -> ReviewerState:
    return ReviewerState(
        job=job.model_dump(),
        result=None,
        docs_updated=False,
    )
