import logging
import subprocess
import time

from claude_agent import ClaudeAgent
from config import Config, ProjectConfig
from graph import build_graph, initial_state
from langgraph.graph.state import CompiledStateGraph
from repo_manager import sync_all
from shared.constants import (
    LABEL_DONE,
    LABEL_IN_PROGRESS,
    LABEL_PROJECT_PREFIX,
)
from shared.forgejo import ForgejoClient
from shared.models import ForgejoIssue
from shared.queue import RedisQueue

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger(__name__)


def configure_git(name: str, email: str) -> None:
    subprocess.run(["git", "config", "--global", "user.name", name], check=True)
    subprocess.run(["git", "config", "--global", "user.email", email], check=True)


def get_project_name(labels: list[str]) -> str | None:
    for label in labels:
        if label.startswith(LABEL_PROJECT_PREFIX):
            return label.removeprefix(LABEL_PROJECT_PREFIX)
    return None


def should_skip(issue: ForgejoIssue, queue: RedisQueue, owner: str, repo: str) -> bool:
    if queue.is_processed(f"{owner}/{repo}#{issue.number}"):
        return True
    if LABEL_IN_PROGRESS in issue.labels or LABEL_DONE in issue.labels:
        return True
    return False


def start_issue(forgejo: ForgejoClient, owner: str, repo: str, issue: ForgejoIssue) -> None:
    label_id = forgejo.get_label_id(owner, repo, LABEL_IN_PROGRESS)
    if label_id is not None:
        forgejo.add_label(owner, repo, issue.number, label_id)
    forgejo.create_comment(
        owner, repo, issue.number,
        f"AI orchestrator started implementation on branch `ai/issue-{issue.number}`.",
    )


def process_issue(
    issue: ForgejoIssue,
    project: ProjectConfig,
    forgejo: ForgejoClient,
    queue: RedisQueue,
    graph: CompiledStateGraph,
    issue_owner: str,
    issue_repo: str,
) -> None:
    code_owner, code_repo = project.repo.split("/", 1)
    start_issue(forgejo, issue_owner, issue_repo, issue)
    log.info("processing issue #%d → %s", issue.number, project.repo)
    graph.invoke(initial_state(
        issue=issue,
        code_repo_owner=code_owner,
        code_repo_name=code_repo,
        project_path=project.path,
        base_branch=project.base_branch,
    ))
    queue.mark_processed(f"{issue_owner}/{issue_repo}#{issue.number}")


def poll_repo(
    owner: str,
    repo: str,
    forgejo: ForgejoClient,
    queue: RedisQueue,
    graph: CompiledStateGraph,
    config: Config,
) -> None:
    try:
        issues = forgejo.get_issues(owner=owner,repo=repo, assignee=config.forgejo_assignee)
    except Exception as exc:
        log.error("fetch issues failed for %s/%s: %s", owner, repo, exc)
        return

    for issue in issues:
        if should_skip(issue, queue, owner, repo):
            continue

        project_name = get_project_name(issue.labels)
        if project_name is None:
            log.warning("issue #%d has no project:<name> label, skipping", issue.number)
            continue

        project = config.projects.get(project_name)
        if project is None:
            log.error("unknown project '%s' on issue #%d", project_name, issue.number)
            continue

        try:
            process_issue(issue, project, forgejo, queue, graph, owner, repo)
        except Exception as exc:
            log.error("issue #%d failed: %s", issue.number, exc)


def main() -> None:
    config = Config()
    configure_git(config.git_user_name, config.git_user_email)

    forgejo = ForgejoClient(config.forgejo_url, config.forgejo_token)
    queue = RedisQueue(config.redis_url)
    graph = build_graph(ClaudeAgent(config.anthropic_api_key), forgejo, queue, config)

    sync_all(config.projects, config.forgejo_url, config.forgejo_token)

    log.info("orchestrator started, poll every %ds", config.poll_interval)

    while True:
        for repo_ref in config.forgejo_issue_repos:
            owner, repo = repo_ref.split("/", 1)
            poll_repo(owner, repo, forgejo, queue, graph, config)
        time.sleep(config.poll_interval)


if __name__ == "__main__":
    main()
