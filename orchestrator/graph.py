import logging
from typing import TypedDict

from langgraph.graph import END, StateGraph
from langgraph.graph.state import CompiledStateGraph

from claude_agent import ClaudeAgent
from config import Config
from shared.constants import LABEL_DONE
from shared.forgejo import ForgejoClient
from shared.git_ops import GitOps
from shared.models import ForgejoIssue, ReviewJob
from shared.queue import RedisQueue
from test_runner import run_tests


log = logging.getLogger(__name__)

MAX_IMPL_ATTEMPTS = 3


class OrchestratorState(TypedDict):
    issue: ForgejoIssue
    code_repo_owner: str
    code_repo_name: str
    project_path: str
    base_branch: str
    branch: str
    pr_number: int | None
    diff: str | None
    impl_attempts: int
    test_error: str | None
    failed: bool


class _ImplUpdate(TypedDict, total=False):
    impl_attempts: int
    test_error: str | None


class _TestUpdate(TypedDict, total=False):
    failed: bool
    test_error: str | None


class _PRUpdate(TypedDict, total=False):
    pr_number: int
    diff: str


def build_graph(
    agent: ClaudeAgent,
    forgejo: ForgejoClient,
    queue: RedisQueue,
    config: Config,
) -> CompiledStateGraph:
    def implement(state: OrchestratorState) -> _ImplUpdate:
        if state["impl_attempts"] == 0:
            git = _code_git(state, config)
            git.checkout(state["base_branch"])
            git.create_branch(state["branch"])
        else:
            log.info("retry impl attempt %d for issue #%d", state["impl_attempts"] + 1, state["issue"].number)

        agent.implement_issue(state["issue"], state["project_path"], state["test_error"])
        return {"impl_attempts": state["impl_attempts"] + 1, "test_error": None}

    def run_tests_node(state: OrchestratorState) -> _TestUpdate:
        if not _code_git(state, config).has_changes():
            log.warning("no changes after impl for issue #%d", state["issue"].number)
            return {"failed": True}

        success, error = run_tests(state["project_path"])
        if success:
            return {"test_error": None}
        log.warning("tests failed (attempt %d): %s", state["impl_attempts"], error[:200])
        return {"test_error": error}

    def route_after_tests(state: OrchestratorState) -> str:
        if state.get("failed"):
            return END
        if state["test_error"] is not None:
            if state["impl_attempts"] >= MAX_IMPL_ATTEMPTS:
                log.error("max impl attempts reached for issue #%d", state["issue"].number)
                return END
            return "implement"
        return "create_pr"

    def create_pr(state: OrchestratorState) -> _PRUpdate:
        issue = state["issue"]
        git = _code_git(state, config)

        git.add_all()
        git.commit(f"feat: implement issue #{issue.number} - {issue.title}")
        git.push(state["branch"])

        pr = forgejo.create_pr(
            state["code_repo_owner"],
            state["code_repo_name"],
            title=f"feat: {issue.title}",
            body=(
                f"Implements [{issue.repo_owner}/{issue.repo_name}#{issue.number}]"
                f"({config.forgejo_url}/{issue.repo_owner}/{issue.repo_name}/issues/{issue.number})"
                f"\n\nImplemented by AI orchestrator."
            ),
            head=state["branch"],
            base=state["base_branch"],
        )

        diff = forgejo.get_pr_diff(state["code_repo_owner"], state["code_repo_name"], pr.number)
        return {"pr_number": pr.number, "diff": diff}

    def queue_review(state: OrchestratorState) -> _ImplUpdate:
        issue = state["issue"]
        pr_number = state["pr_number"]
        diff = state["diff"]
        if pr_number is None or diff is None:
            raise RuntimeError("pr_number or diff missing in queue_review")

        done_id = forgejo.get_label_id(issue.repo_owner, issue.repo_name, LABEL_DONE)
        if done_id is not None:
            forgejo.add_label(issue.repo_owner, issue.repo_name, issue.number, done_id)

        forgejo.create_comment(
            issue.repo_owner,
            issue.repo_name,
            issue.number,
            f"PR created: [{state['code_repo_owner']}/{state['code_repo_name']}#{pr_number}]"
            f"({config.forgejo_url}/{state['code_repo_owner']}/{state['code_repo_name']}/pulls/{pr_number})",
        )

        job = ReviewJob(
            repo_owner=state["code_repo_owner"],
            repo_name=state["code_repo_name"],
            pr_number=pr_number,
            pr_title=f"feat: {issue.title}",
            pr_branch=state["branch"],
            project_path=state["project_path"],
            issue_number=issue.number,
            diff=diff,
        )
        queue.push("queue:review", job)

        _code_git(state, config).checkout(state["base_branch"])

        log.info("issue #%d → PR %s/%s#%d queued for review", issue.number, state["code_repo_owner"], state["code_repo_name"], pr_number)
        return {}

    g: StateGraph = StateGraph(OrchestratorState)
    g.add_node("implement", implement)
    g.add_node("run_tests", run_tests_node)
    g.add_node("create_pr", create_pr)
    g.add_node("queue_review", queue_review)

    g.set_entry_point("implement")
    g.add_edge("implement", "run_tests")
    g.add_conditional_edges("run_tests", route_after_tests, {
        "implement": "implement",
        "create_pr": "create_pr",
        END: END,
    })
    g.add_edge("create_pr", "queue_review")
    g.add_edge("queue_review", END)

    return g.compile()


def initial_state(
    issue: ForgejoIssue,
    code_repo_owner: str,
    code_repo_name: str,
    project_path: str,
    base_branch: str,
) -> OrchestratorState:
    return OrchestratorState(
        issue=issue,
        code_repo_owner=code_repo_owner,
        code_repo_name=code_repo_name,
        project_path=project_path,
        base_branch=base_branch,
        branch=f"ai/issue-{issue.number}",
        pr_number=None,
        diff=None,
        impl_attempts=0,
        test_error=None,
        failed=False,
    )


def _code_git(state: OrchestratorState, config: Config) -> GitOps:
    return GitOps(
        state["project_path"],
        config.forgejo_url,
        config.forgejo_token,
        state["code_repo_owner"],
        state["code_repo_name"],
    )
