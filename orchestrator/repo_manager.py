import subprocess
from pathlib import Path

import structlog

from config import ProjectConfig

log = structlog.get_logger(__name__)


def _remote_url(forgejo_url: str, token: str, repo: str) -> str:
    base = forgejo_url.rstrip("/")
    return base.replace("https://", f"https://oauth2:{token}@") + f"/{repo}.git"


def clone_or_pull(project: ProjectConfig, forgejo_url: str, token: str, base_branch: str) -> None:
    path = Path(project.path)
    remote = _remote_url(forgejo_url, token, project.repo)

    if path.exists():
        log.info("pulling repo", repo=project.repo)
        subprocess.run(["git", "checkout", base_branch], cwd=path, check=True)
        subprocess.run(["git", "pull", remote, base_branch], cwd=path, check=True)
    else:
        log.info("cloning repo", repo=project.repo, path=str(path))
        path.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(["git", "clone", remote, str(path)], check=True)


def sync_all(
    projects: dict[str, ProjectConfig],
    forgejo_url: str,
    token: str,
) -> None:
    for name, project in projects.items():
        try:
            clone_or_pull(project, forgejo_url, token, project.base_branch)
        except subprocess.CalledProcessError as exc:
            log.error("sync failed", project=name, error=str(exc))
