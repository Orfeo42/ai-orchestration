import json

import redis
from pydantic import BaseModel


class RedisQueue:
    def __init__(self, url: str) -> None:
        self._r = redis.from_url(url, decode_responses=True)

    def push(self, queue_name: str, job: BaseModel) -> None:
        self._r.lpush(queue_name, job.model_dump_json())

    def pop(self, queue_name: str, timeout: int = 0) -> dict | None:
        result = self._r.brpop(queue_name, timeout=timeout)
        if result is None:
            return None
        _, data = result
        return json.loads(data)

    def is_processed(self, key: str) -> bool:
        return bool(self._r.sismember("processed", key))

    def mark_processed(self, key: str) -> None:
        self._r.sadd("processed", key)
