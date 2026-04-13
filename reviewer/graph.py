import structlog
from typing import TypedDict

from langgraph.graph import END, StateGraph
from langgraph.graph.state import CompiledStateGraph

from claude_reviewer import ClaudeReviewer
from docs_writer import DocsWriter
from gemini_reviewer import GeminiReviewer
from shared.forgejo import ForgejoClient
from shared.git_ops import GitOps
from shared.models import ReviewJob, ReviewResult

log = structlog.get_logger(__name__)


class ReviewerState(TypedDict):
    job: ReviewJob
    result: ReviewResult | None
    docs_updated: bool


class _ReviewUpdate(TypedDict, total=False):
    result: ReviewResult


class _DocsUpdate(TypedDict, total=False):
    docs_updated: bool


def build_graph(
    reviewer: ClaudeReviewer | GeminiReviewer,
    docs_writer: DocsWriter,
    forgejo: ForgejoClient,
    forgejo_url: str,
    forgejo_token: str,
) -> CompiledStateGraph:
    def review(state: ReviewerState) -> _ReviewUpdate:
        result = reviewer.review(state["job"])
        log.info("reviewed PR", pr_number=state["job"].pr_number, approved=result.approved)
        return {"result": result}

    def update_docs(state: ReviewerState) -> _DocsUpdate:
        job = state["job"]
        result = state["result"]
        if result is None:
            raise RuntimeError("result missing in update_docs")

        git = GitOps(job.project_path, forgejo_url, forgejo_token, job.repo_owner, job.repo_name)
        git.fetch()
        git.checkout_remote_branch(job.pr_branch)

        updated = docs_writer.update_readme(job, result)
        if updated:
            git.add("README.md")
            git.commit(f"docs: update README for PR #{job.pr_number}")
            git.push(job.pr_branch)
            log.info("README updated", pr_number=job.pr_number)

        return {"docs_updated": updated}

    def post_review(state: ReviewerState) -> _DocsUpdate:
        job = state["job"]
        result = state["result"]
        if result is None:
            raise RuntimeError("result missing in post_review")

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

    g: StateGraph = StateGraph(ReviewerState)
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
        job=job,
        result=None,
        docs_updated=False,
    )
