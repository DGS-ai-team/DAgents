from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True, slots=True)
class ToolApprovalRequest:
    call_id: str
    name: str
    arguments: dict[str, Any]
    raw_arguments: str
    approval_reason: str = ""
    risk_level: str = ""
    approval_mode: str = ""


@dataclass(frozen=True, slots=True)
class ApprovalDecision:
    approved: list[str]
    rejected: list[str]

    def to_resume_value(self) -> dict[str, Any]:
        return {"type": "selection", "approved": self.approved, "rejected": self.rejected}


def extract_tool_approval_requests(data: dict[str, Any]) -> list[ToolApprovalRequest]:
    args = data.get("approval_args")
    if not isinstance(args, dict):
        return []
    raw_calls = args.get("tool_calls")
    if not isinstance(raw_calls, list):
        return []
    requests: list[ToolApprovalRequest] = []
    for raw in raw_calls:
        if not isinstance(raw, dict):
            continue
        call_id = str(raw.get("id") or "").strip()
        name = str(raw.get("name") or "").strip() or "unknown"
        if not call_id:
            continue
        arguments = raw.get("arguments")
        if not isinstance(arguments, dict):
            arguments = {}
        raw_arguments = raw.get("raw_arguments")
        if not isinstance(raw_arguments, str) or not raw_arguments.strip():
            raw_arguments = json.dumps(arguments, ensure_ascii=False)
        requests.append(
            ToolApprovalRequest(
                call_id=call_id,
                name=name,
                arguments=arguments,
                raw_arguments=raw_arguments,
                approval_reason=str(raw.get("approval_reason") or ""),
                risk_level=str(raw.get("risk_level") or ""),
                approval_mode=str(raw.get("approval_mode") or ""),
            )
        )
    return requests


def build_all_approved_decision(requests: list[ToolApprovalRequest]) -> ApprovalDecision:
    return ApprovalDecision(approved=[item.call_id for item in requests], rejected=[])


def build_all_rejected_decision(requests: list[ToolApprovalRequest]) -> ApprovalDecision:
    return ApprovalDecision(approved=[], rejected=[item.call_id for item in requests])


def build_selection_decision(requests: list[ToolApprovalRequest], approved_ids: set[str]) -> ApprovalDecision:
    known_ids = [item.call_id for item in requests]
    unknown = approved_ids - set(known_ids)
    if unknown:
        raise ValueError(f"unknown tool call id: {', '.join(sorted(unknown))}")
    approved = [call_id for call_id in known_ids if call_id in approved_ids]
    rejected = [call_id for call_id in known_ids if call_id not in approved_ids]
    return ApprovalDecision(approved=approved, rejected=rejected)


def parse_selection_tokens(value: str, requests: list[ToolApprovalRequest]) -> set[str]:
    tokens = [part.strip() for part in value.replace(",", " ").split() if part.strip()]
    by_index = {str(index): item.call_id for index, item in enumerate(requests, start=1)}
    by_id = {item.call_id: item.call_id for item in requests}
    selected: set[str] = set()
    invalid: list[str] = []
    for token in tokens:
        call_id = by_index.get(token) or by_id.get(token)
        if call_id is None:
            invalid.append(token)
        else:
            selected.add(call_id)
    if invalid:
        raise ValueError(f"unknown selection: {', '.join(invalid)}")
    return selected
