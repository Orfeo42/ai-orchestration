from typing import TypeVar, cast

import redis
import structlog
from pydantic import BaseModel

_T = TypeVar("_T", bound=BaseModel)
log = structlog.get_logger(__name__)


class RedisQueue:
    def __init__(self, url: str) -> None:
        self._r = redis.from_url(url, decode_responses=True)

    def push(self, queue_name: str, job: BaseModel) -> None:
        log.debug("pushing to queue", queue=queue_name)
        self._r.lpush(queue_name, job.model_dump_json())

    def pop_as(self, queue_name: str, model: type[_T], timeout: int = 0) -> _T | None:
        raw = self._r.brpop(queue_name, timeout=timeout)
        if raw is None:
            return None
        result = cast(tuple[str, str], raw)
        _, data = result
        log.debug("popped from queue", queue=queue_name)
        return model.model_validate_json(data)

    def is_processed(self, key: str) -> bool:
        return bool(self._r.sismember("processed", key))

    def mark_processed(self, key: str) -> None:
        log.info("marking issue processed", key=key)
        self._r.sadd("processed", key)

    def mark_in_progress(self, key: str, ttl_seconds: int = 7200) -> None:
        log.info("marking issue in-progress", key=key, ttl_seconds=ttl_seconds)
        self._r.set(f"in_progress:{key}", "1", ex=ttl_seconds)

    def is_in_progress(self, key: str) -> bool:
        result = self._r.exists(f"in_progress:{key}") == 1
        log.debug("checked in-progress", key=key, result=result)
        return result

    def clear_in_progress(self, key: str) -> None:
        log.info("clearing in-progress lock", key=key)
        self._r.delete(f"in_progress:{key}")
