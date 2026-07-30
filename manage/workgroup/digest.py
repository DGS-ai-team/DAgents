"""Canonical JSON + sha256 digest（D0.5 §1.4）。"""

from __future__ import annotations

import hashlib
import json
from typing import Any


_EXCLUDE_HASH_KEYS = frozenset({"digest", "payload_hash"})


def _canonicalize(value: Any) -> Any:
    if isinstance(value, dict):
        return {
            str(k): _canonicalize(value[k])
            for k in sorted(value.keys(), key=lambda x: str(x))
            if str(k) not in _EXCLUDE_HASH_KEYS
        }
    if isinstance(value, list):
        return [_canonicalize(item) for item in value]
    return value


def canonical_json_bytes(value: Any) -> bytes:
    """UTF-8 紧凑 JSON；对象键递归字典序；不含 digest/payload_hash。"""
    return json.dumps(
        _canonicalize(value),
        ensure_ascii=False,
        separators=(",", ":"),
        allow_nan=False,
    ).encode("utf-8")


def sha256_digest(value: Any) -> str:
    digest = hashlib.sha256(canonical_json_bytes(value)).hexdigest()
    return f"sha256:{digest}"
