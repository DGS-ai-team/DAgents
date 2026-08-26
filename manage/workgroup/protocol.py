"""Stable protocol constants shared by the Manage workgroup boundaries."""

from __future__ import annotations

from typing import Any

from manage.workgroup.errors import WorkgroupError

PROTOCOL_VERSION = "1"
SCHEMA_VERSION = "0.5.0"
SUPPORTED_PROTOCOL_VERSIONS = frozenset({PROTOCOL_VERSION})

# Keep this list deliberately small and stable.  It describes protocol
# features, not every tool or route exposed by a particular Node.
MANAGE_CAPABILITIES = (
    "fencing",
    "idempotency",
    "resume",
    "timeline",
)


def validate_protocol_version(value: Any) -> str:
    """Return a normalized supported version or raise a protocol error."""

    version = str(value or PROTOCOL_VERSION).strip()
    if version not in SUPPORTED_PROTOCOL_VERSIONS:
        raise WorkgroupError(
            "schema_mismatch",
            f"unsupported protocol_version {version!r}",
            http_status=400,
        )
    return version


def validate_schema_version(value: Any) -> str:
    """Reject a peer that does not speak the current workgroup schema."""

    version = str(value or SCHEMA_VERSION).strip()
    if version != SCHEMA_VERSION:
        raise WorkgroupError(
            "schema_mismatch",
            f"unsupported schema_version {version!r}",
            http_status=400,
        )
    return version


def normalize_capabilities(value: Any) -> list[str]:
    """Normalize peer capabilities without allowing unbounded label values."""

    if not isinstance(value, (list, tuple, set, frozenset)):
        return []
    return sorted(
        {
            item.strip()
            for item in (str(item) for item in value)
            if item.strip()
        }
    )
