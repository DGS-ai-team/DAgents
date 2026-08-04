"""稳定错误码（对齐 D0.5 §7.4）。"""

from __future__ import annotations


class WorkgroupError(Exception):
    def __init__(
        self,
        code: str,
        message: str,
        *,
        http_status: int = 400,
        retryable: bool = False,
        details: dict | None = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.message = message
        self.http_status = http_status
        self.retryable = retryable
        self.details = details or {}

    def as_body(self) -> dict:
        body: dict = {"code": self.code, "message": self.message, "retryable": self.retryable}
        if self.details:
            body["details"] = self.details
        return body
