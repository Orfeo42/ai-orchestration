import logging
import subprocess

from claude_reviewer import ClaudeReviewer
from config import Config
from docs_writer import DocsWriter
from gemini_reviewer import GeminiReviewer
from graph import build_graph, initial_state
from shared.forgejo import ForgejoClient
from shared.models import ReviewJob
from shared.queue import RedisQueue

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger(__name__)


def configure_git(name: str, email: str) -> None:
    subprocess.run(["git", "config", "--global", "user.name", name], check=True)
    subprocess.run(["git", "config", "--global", "user.email", email], check=True)


def main() -> None:
    config = Config()
    configure_git(config.git_user_name, config.git_user_email)

    forgejo = ForgejoClient(config.forgejo_url, config.forgejo_token)
    queue = RedisQueue(config.redis_url)
    docs_writer = DocsWriter(config.anthropic_api_key)

    if config.primary_reviewer == "claude":
        reviewer: ClaudeReviewer | GeminiReviewer = ClaudeReviewer(config.anthropic_api_key)
    else:
        reviewer = GeminiReviewer(config.gemini_api_key)

    graph = build_graph(reviewer, docs_writer, forgejo, config.forgejo_url, config.forgejo_token)

    log.info("reviewer started (primary=%s)", config.primary_reviewer)

    try:
        while True:
            raw = queue.pop("queue:review", timeout=10)
            if raw is None:
                continue

            job = ReviewJob.model_validate(raw)
            log.info("reviewing PR #%d for %s/%s", job.pr_number, job.repo_owner, job.repo_name)
            try:
                graph.invoke(initial_state(job))
            except Exception as exc:
                log.error("review failed for PR #%d: %s", job.pr_number, exc)
    finally:
        forgejo.close()


if __name__ == "__main__":
    main()
