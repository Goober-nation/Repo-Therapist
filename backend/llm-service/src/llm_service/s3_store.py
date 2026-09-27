"""Read-only S3-compatible object store for Repository service snapshots."""

from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass(frozen=True)
class S3Config:
    endpoint: str = "localhost:9000"
    bucket: str = "repo-content"
    access_key: str = field(default="", repr=False)
    secret_key: str = field(default="", repr=False)
    use_ssl: bool = False
    connect_timeout: float = 5
    read_timeout: float = 15

    @classmethod
    def from_env(cls) -> S3Config:
        ssl = os.environ.get("S3_USE_SSL", "false").lower()
        if ssl not in {"true", "false"}:
            raise ValueError("S3_USE_SSL must be true or false")
        return cls(
            endpoint=os.environ.get("S3_ENDPOINT", "localhost:9000"),
            bucket=os.environ.get("S3_BUCKET", "repo-content"),
            access_key=os.environ.get("S3_ACCESS_KEY", ""),
            secret_key=os.environ.get("S3_SECRET_KEY", ""),
            use_ssl=ssl == "true",
            connect_timeout=_seconds_env("S3_CONNECT_TIMEOUT", cls.connect_timeout),
            read_timeout=_seconds_env("S3_READ_TIMEOUT", cls.read_timeout),
        )

    def __post_init__(self) -> None:
        if (not self.endpoint or "://" in self.endpoint or "/" in self.endpoint or
                not self.bucket or not self.access_key or not self.secret_key):
            raise ValueError("S3 endpoint, bucket and credentials are required")
        if self.connect_timeout <= 0 or self.read_timeout <= 0:
            raise ValueError("S3 timeouts must be positive")


def _seconds_env(key: str, default: float) -> float:
    value = os.environ.get(key, "").strip()
    if not value:
        return default
    try:
        return float(value)
    except ValueError:
        raise ValueError(f"{key} must be a number of seconds") from None


class S3Store:
    """Implement snapshot.ObjectStore without creating buckets or writing objects."""

    def __init__(self, config: S3Config, client=None):
        self.bucket = config.bucket
        if client is None:
            import boto3
            from botocore.config import Config

            client = boto3.client(
                "s3", endpoint_url=f"{'https' if config.use_ssl else 'http'}://{config.endpoint}",
                aws_access_key_id=config.access_key, aws_secret_access_key=config.secret_key,
                region_name="us-east-1", config=Config(s3={"addressing_style": "path"},
                                                     connect_timeout=config.connect_timeout,
                                                     read_timeout=config.read_timeout,
                                                     retries={"mode": "standard", "total_max_attempts": 2}),
            )
        self.client = client

    def list_objects(self, prefix: str) -> dict[str, tuple[int, str]]:
        objects = {}
        pages = self.client.get_paginator("list_objects_v2").paginate(
            Bucket=self.bucket, Prefix=prefix)
        for page in pages:
            for item in page.get("Contents", []):
                key = item["Key"]
                if key in objects or not key.startswith(prefix):
                    raise ValueError("duplicate or out-of-prefix S3 object")
                objects[key] = (item["Size"], item["ETag"])
        return objects

    def read_object(self, key: str) -> bytes:
        response = self.client.get_object(Bucket=self.bucket, Key=key)
        body = response["Body"]
        try:
            return body.read(2_000_001)
        finally:
            body.close()
