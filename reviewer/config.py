import os


class Config:
    anthropic_api_key: str
    gemini_api_key: str
    redis_url: str
    forgejo_url: str
    forgejo_token: str
    primary_reviewer: str
    git_user_name: str
    git_user_email: str

    def __init__(self) -> None:
        self.anthropic_api_key = os.environ["ANTHROPIC_API_KEY"]
        self.gemini_api_key = os.environ["GEMINI_API_KEY"]
        self.redis_url = os.environ.get("REDIS_URL", "redis://redis:6379")
        self.forgejo_url = os.environ["FORGEJO_URL"]
        self.forgejo_token = os.environ["FORGEJO_TOKEN"]
        self.primary_reviewer = os.environ.get("PRIMARY_REVIEWER", "gemini")
        self.git_user_name = os.environ.get("GIT_USER_NAME", "AI Reviewer")
        self.git_user_email = os.environ.get("GIT_USER_EMAIL", "ai-reviewer@orchestrator.local")
