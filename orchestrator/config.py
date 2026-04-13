import json
import os
from pathlib import Path

from pydantic import BaseModel, TypeAdapter, computed_field

PROJECTS_ROOT = Path("/projects")


class ProjectConfig(BaseModel):
    repo: str
    base_branch: str = "main"

    @computed_field
    @property
    def path(self) -> str:
        return str(PROJECTS_ROOT / self.repo.split("/", 1)[1])


_projects_adapter = TypeAdapter(dict[str, ProjectConfig])


class Config:
    forgejo_url: str
    forgejo_token: str
    forgejo_issue_repos: list[str]
    forgejo_assignee: str | None
    redis_url: str
    projects: dict[str, ProjectConfig]
    poll_interval: int
    claude_timeout: int
    git_user_name: str
    git_user_email: str

    def __init__(self) -> None:
        self.forgejo_url = os.environ["FORGEJO_URL"]
        self.forgejo_token = os.environ["FORGEJO_TOKEN"]
        self.forgejo_issue_repos = [r.strip() for r in os.environ["FORGEJO_ISSUE_REPOS"].split(",")]
        self.forgejo_assignee = os.environ.get("FORGEJO_ASSIGNEE")
        self.redis_url = os.environ.get("REDIS_URL", "redis://redis:6379")
        self.poll_interval = int(os.environ.get("POLL_INTERVAL", "60"))
        self.claude_timeout = int(os.environ.get("CLAUDE_TIMEOUT", "3600"))
        self.git_user_name = os.environ.get("GIT_USER_NAME", "AI Orchestrator")
        self.git_user_email = os.environ.get("GIT_USER_EMAIL", "ai@orchestrator.local")

        config_path = Path(os.environ.get("PROJECTS_CONFIG", "/config/projects.json"))
        with config_path.open() as f:
            self.projects = _projects_adapter.validate_python(json.load(f))
