import httpx
import structlog

from shared.constants import LABEL_IMPLEMENT
from shared.models import ForgejoPR, ForgejoIssue

log = structlog.get_logger(__name__)


class ForgejoClient:
    def __init__(self, base_url: str, token: str) -> None:
        self._client = httpx.Client(
            base_url=base_url,
            headers={"Authorization": f"token {token}"},
            timeout=30,
        )

    def get_issues(
        self,
        owner: str,
        repo: str,
        label: str = LABEL_IMPLEMENT,
        assignee: str | None = None,
    ) -> list[ForgejoIssue]:
        params: dict[str, str | int] = {
            "state": "open",
            "type": "issues",
            "labels": label,
            "limit": 50,
        }
        if assignee is not None:
            params["assignee"] = assignee
        
        log.debug("fetching issues", repo=f"{owner}/{repo}", label=label)
        resp = self._client.get(
            f"/api/v1/repos/{owner}/{repo}/issues",
            params=params,
        )
        resp.raise_for_status()
        issues = []
        for raw in resp.json():
            labels = [lbl["name"] for lbl in raw.get("labels", [])]
            issues.append(ForgejoIssue(
                id=raw["id"],
                number=raw["number"],
                title=raw["title"],
                body=raw.get("body", ""),
                repo_owner=owner,
                repo_name=repo,
                labels=labels,
            ))
        return issues

    def get_label_id(self, owner: str, repo: str, label_name: str) -> int | None:
        resp = self._client.get(f"/api/v1/repos/{owner}/{repo}/labels")
        resp.raise_for_status()
        for label in resp.json():
            if label["name"] == label_name:
                return label["id"]
        return None

    def add_label(self, owner: str, repo: str, issue_number: int, label_id: int) -> None:
        resp = self._client.post(
            f"/api/v1/repos/{owner}/{repo}/issues/{issue_number}/labels",
            json={"labels": [label_id]},
        )
        resp.raise_for_status()

    def create_comment(self, owner: str, repo: str, issue_number: int, body: str) -> None:
        if True:
            return
        resp = self._client.post(
            f"/api/v1/repos/{owner}/{repo}/issues/{issue_number}/comments",
            json={"body": body},
        )
        resp.raise_for_status()

    def create_pr(
        self,
        owner: str,
        repo: str,
        title: str,
        body: str,
        head: str,
        base: str = "main",
    ) -> ForgejoPR:
        resp = self._client.post(
            f"/api/v1/repos/{owner}/{repo}/pulls",
            json={"title": title, "body": body, "head": head, "base": base},
        )
        resp.raise_for_status()
        return ForgejoPR.model_validate(resp.json())

    def get_pr_diff(self, owner: str, repo: str, pr_number: int) -> str:
        resp = self._client.get(f"/api/v1/repos/{owner}/{repo}/pulls/{pr_number}.diff")
        resp.raise_for_status()
        return resp.text

    def create_pr_review(
        self,
        owner: str,
        repo: str,
        pr_number: int,
        body: str,
        approved: bool,
    ) -> None:
        resp = self._client.post(
            f"/api/v1/repos/{owner}/{repo}/pulls/{pr_number}/reviews",
            json={"body": body, "event": "APPROVED" if approved else "REQUEST_CHANGES"},
        )
        resp.raise_for_status()

    def close(self) -> None:
        self._client.close()
