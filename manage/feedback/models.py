from __future__ import annotations
from typing import Literal
from pydantic import BaseModel, Field

Category = Literal["bug", "suggestion", "other"]
Status = Literal["open", "in_progress", "resolved", "closed"]


class FeedbackCreate(BaseModel):
    client_feedback_id: str = Field(min_length=1, max_length=128)
    node_id: str = Field(min_length=1, max_length=256)
    category: Category
    title: str = Field(min_length=1, max_length=256)
    body: str = Field(min_length=1, max_length=100_000)
    created_at: str = ""


class FeedbackRecord(FeedbackCreate):
    status: Status = "open"
    reply: str = ""
    revision: int = 1
    updated_at: str = ""


class FeedbackPatch(BaseModel):
    status: Status | None = None
    reply: str | None = Field(default=None, max_length=100_000)
    revision: int = Field(ge=1)
