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
        )

    def __post_init__(self) -> None:
        if (not self.endpoint or "://" in self.endpoint or "/" in self.endpoint or
                not self.bucket or not self.access_key or not self.secret_key):
            raise ValueError("S3 endpoint, bucket and credentials are required")


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
                region_name="us-east-1", config=Config(s3={"addressing_style": "path"}),
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
