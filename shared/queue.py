from typing import TypeVar

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
        result = self._r.brpop(queue_name, timeout=timeout)
        if result is None:
            return None
        _, data = result
        log.debug("popped from queue", queue=queue_name)
        return model.model_validate_json(data)

    def is_processed(self, key: str) -> bool:
        return bool(self._r.sismember("processed", key))

    def mark_processed(self, key: str) -> None:
        self._r.sadd("processed", key)
