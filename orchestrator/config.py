import json
import os
from pathlib import Path

from pydantic import BaseModel, TypeAdapter


class ProjectConfig(BaseModel):
    path: str
    base_branch: str = "main"


_projects_adapter = TypeAdapter(dict[str, ProjectConfig])


class Config:
    forgejo_url: str
    forgejo_token: str
    forgejo_repos: list[str]
    anthropic_api_key: str
    redis_url: str
    projects: dict[str, ProjectConfig]
    poll_interval: int
    git_user_name: str
    git_user_email: str

    def __init__(self) -> None:
        self.forgejo_url = os.environ["FORGEJO_URL"]
        self.forgejo_token = os.environ["FORGEJO_TOKEN"]
        self.forgejo_repos = [r.strip() for r in os.environ["FORGEJO_REPOS"].split(",")]
        self.anthropic_api_key = os.environ["ANTHROPIC_API_KEY"]
        self.redis_url = os.environ.get("REDIS_URL", "redis://redis:6379")
        self.poll_interval = int(os.environ.get("POLL_INTERVAL", "60"))
        self.git_user_name = os.environ.get("GIT_USER_NAME", "AI Orchestrator")
        self.git_user_email = os.environ.get("GIT_USER_EMAIL", "ai@orchestrator.local")

        config_path = Path(os.environ.get("PROJECTS_CONFIG", "/config/projects.json"))
        with config_path.open() as f:
            self.projects = _projects_adapter.validate_python(json.load(f))
