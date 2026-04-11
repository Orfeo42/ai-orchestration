import logging
import subprocess
import time

from claude_agent import ClaudeAgent
from config import Config
from graph import build_graph, initial_state
from shared.forgejo import ForgejoClient
from shared.queue import RedisQueue

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger(__name__)

LABEL_IMPLEMENT = "ai-implement"
LABEL_IN_PROGRESS = "ai-in-progress"
LABEL_DONE = "ai-done"


def configure_git(name: str, email: str) -> None:
    subprocess.run(["git", "config", "--global", "user.name", name], check=True)
    subprocess.run(["git", "config", "--global", "user.email", email], check=True)


def main() -> None:
    config = Config()
    configure_git(config.git_user_name, config.git_user_email)

    forgejo = ForgejoClient(config.forgejo_url, config.forgejo_token)
    agent = ClaudeAgent(config.anthropic_api_key)
    queue = RedisQueue(config.redis_url)
    graph = build_graph(agent, forgejo, queue, config)

    log.info("orchestrator started, poll every %ds", config.poll_interval)

    try:
        while True:
            for repo_ref in config.forgejo_repos:
                owner, repo = repo_ref.split("/", 1)
                try:
                    issues = forgejo.get_issues(owner, repo)
                except Exception as exc:
                    log.error("fetch issues failed for %s: %s", repo_ref, exc)
                    continue

                for issue in issues:
                    issue_key = f"{owner}/{repo}#{issue.number}"
                    if queue.is_processed(issue_key):
                        continue
                    if LABEL_IN_PROGRESS in issue.labels or LABEL_DONE in issue.labels:
                        continue

                    project_info = config.projects.get(f"{owner}/{repo}")
                    if project_info is None:
                        log.error("no project config for %s", repo_ref)
                        continue

                    in_progress_id = forgejo.get_label_id(owner, repo, LABEL_IN_PROGRESS)
                    if in_progress_id is not None:
                        forgejo.add_label(owner, repo, issue.number, in_progress_id)

                    forgejo.create_comment(
                        owner, repo, issue.number,
                        f"AI orchestrator started implementation on branch `ai/issue-{issue.number}`.",
                    )

                    log.info("processing issue #%d: %s", issue.number, issue.title)
                    try:
                        state = initial_state(
                            issue,
                            project_info["path"],
                            project_info.get("base_branch", "main"),
                        )
                        graph.invoke(state)
                        queue.mark_processed(issue_key)
                    except Exception as exc:
                        log.error("issue #%d failed: %s", issue.number, exc)

            time.sleep(config.poll_interval)
    finally:
        forgejo.close()


if __name__ == "__main__":
    main()
