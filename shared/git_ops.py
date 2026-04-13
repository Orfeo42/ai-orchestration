import subprocess
from pathlib import Path

import structlog

log = structlog.get_logger(__name__)


class GitOps:
    def __init__(
        self,
        repo_path: str,
        forgejo_url: str,
        token: str,
        owner: str,
        repo: str,
    ) -> None:
        self._path = Path(repo_path)
        self._owner = owner
        self._repo = repo
        remote_url = f"{forgejo_url.rstrip('/')}/{owner}/{repo}.git"
        self._remote_with_auth = remote_url.replace("https://", f"https://oauth2:{token}@")

    def create_branch(self, branch: str) -> None:
        log.info("creating branch", branch=branch, repo=f"{self._owner}/{self._repo}")
        self._run(["git", "checkout", "-b", branch])

    def checkout(self, branch: str) -> None:
        log.info("checking out branch", branch=branch, repo=f"{self._owner}/{self._repo}")
        self._run(["git", "checkout", branch])

    def fetch(self) -> None:
        log.debug("fetching from remote", repo=f"{self._owner}/{self._repo}")
        self._run(["git", "fetch", self._remote_with_auth])

    def checkout_remote_branch(self, branch: str) -> None:
        log.info("checking out remote branch", branch=branch, repo=f"{self._owner}/{self._repo}")
        self._run(["git", "checkout", "-B", branch, f"origin/{branch}"])

    def add(self, path: str) -> None:
        self._run(["git", "add", path])

    def add_all(self) -> None:
        self._run(["git", "add", "-A"])

    def commit(self, message: str) -> None:
        log.info("committing changes", message=message, repo=f"{self._owner}/{self._repo}")
        self._run(["git", "commit", "-m", message])

    def push(self, branch: str) -> None:
        log.info("pushing branch", branch=branch, repo=f"{self._owner}/{self._repo}")
        self._run(["git", "push", self._remote_with_auth, branch])

    def has_changes(self) -> bool:
        result = self._run(["git", "status", "--porcelain"], capture=True)
        return bool(result.stdout.strip())

    def _run(
        self,
        cmd: list[str],
        capture: bool = False,
    ) -> subprocess.CompletedProcess:
        return subprocess.run(
            cmd,
            cwd=self._path,
            capture_output=capture,
            text=True,
            check=True,
        )
