"""异步工具结果仓库：托管协程任务并记录生命周期。"""

from __future__ import annotations

import asyncio
import inspect
from dataclasses import dataclass, replace
from time import time
from typing import Any, Awaitable, Callable
from uuid import uuid4


def _now_ms() -> int:
    """返回当前 Unix 毫秒时间戳。"""
    return int(time() * 1000)


@dataclass(slots=True)
class AsyncToolJob:
    """异步工具任务快照。

    逻辑：
    1. `accepted/running/succeeded/failed/cancelled` 描述任务生命周期；
    2. `result_text/error_text` 仅在终态有意义；
    3. `client_id` 与入站 **`MessageEnvelope.client_id`** 对齐，用于终态回灌时 **`publish` 至正确 SSE 桶**；
    4. `submitted/started/finished` 用于后续观测与排障。
    """

    job_id: str
    session_id: str
    client_id: str
    tool_name: str
    status: str
    submitted_at_unix_ms: int
    started_at_unix_ms: int = 0
    finished_at_unix_ms: int = 0
    result_text: str = ""
    error_text: str = ""


class AsyncToolResultStore:
    """管理异步工具后台任务并提供状态查询。

    逻辑：
    1. `submit_coroutine` 接收协程对象并创建后台 task；
    2. `_run_job` 负责推进状态、记录结果/错误；
    3. 终态触发 `_notify_completed`，支持后续接入自动推送。

    关键边界：
    - 必须在运行中的事件循环内提交；
    - 结果与错误统一转为字符串，便于直接入消息协议；
    - `get_job` 返回副本，避免外部误改仓库状态。
    """

    TERMINAL_STATUSES = {"succeeded", "failed", "cancelled"}

    def __init__(self) -> None:
        """初始化内存仓库与回调列表。"""
        self._jobs: dict[str, AsyncToolJob] = {}
        self._tasks: dict[str, asyncio.Task[None]] = {}
        self._completion_callbacks: list[Callable[[AsyncToolJob], Awaitable[None] | None]] = []
        self._message_queue_sender: Callable[[str, dict[str, Any]], Any] | None = None

    def register_completion_callback(
        self,
        callback: Callable[[AsyncToolJob], Awaitable[None] | None],
    ) -> None:
        """注册任务完成回调。

        逻辑：
        1. 接收一个回调函数；
        2. 任务进入终态后依次调用；
        3. 回调异常只吞日志语义，不影响任务状态。
        """
        self._completion_callbacks.append(callback)

    def register_message_queue_sender(
        self,
        sender: Callable[[str, dict[str, Any]], Any] | None,
    ) -> None:
        """注册 message_queue 发送器（用于投递 `async_tool_result`）。

        逻辑：
        1. 接收一个 **`sender(session_id, payload)`** 回调（可为同步，或返回 **`Awaitable`**）；
        2. **`payload`** 含业务字段及 **`client_id`**（与 **`AsyncToolJob.client_id`** 一致），供 **`MessageEnvelope.client_id`** 路由 SSE；
        3. 任务终态时由 **`_notify_completed`** 调用该回调，若为 awaitable 则 **`await`**；
        4. 传入 **`None`** 可清空发送器，避免服务停止后继续投递。

        关键边界：
        - **`client_id`** 在 **`submit_coroutine`** 阶段已校验非空；若发送器侧仍丢失，由 **`AgentService`** 入队前二次校验。
        """
        self._message_queue_sender = sender

    def submit_coroutine(
        self,
        *,
        session_id: str,
        client_id: str,
        tool_name: str,
        coroutine_obj: Any,
    ) -> AsyncToolJob:
        """提交异步工具协程并立即返回 `accepted` 快照。

        逻辑：
        1. 校验参数是协程对象；
        2. 校验 `session_id` 与 **`client_id`** 非空（后者用于任务终态回灌时路由 SSE）；
        3. 创建 `accepted` 状态任务并在当前事件循环 **`create_task(_run_job)`**。

        关键边界：
        - 非协程对象立即抛 **`TypeError`**；
        - **`client_id`** 仅空白时抛 **`ValueError`**（与「回灌必须可达订阅端」约束一致）；
        - 无运行中事件循环时关闭协程并抛 **`RuntimeError`**。

        与外部交互：
        - 仅内存登记任务；终态时经 **`_message_queue_sender`** 投递（见 **`AgentService`**）。
        """
        if not asyncio.iscoroutine(coroutine_obj):
            raise TypeError("submit_coroutine 仅接受协程对象。")
        final_session_id = str(session_id or "").strip()
        if not final_session_id:
            coroutine_obj.close()
            raise ValueError("session_id 不能为空。")
        final_client_id = str(client_id or "").strip()
        if not final_client_id:
            coroutine_obj.close()
            raise ValueError(
                "client_id 不能为空：异步工具完成后需将 async_tool_result 入队到同一 SSE 通道，"
                "请先通过带 client_id 的入站请求处理本会话（见 OpenAIConversationContext.sse_client_id）。"
            )

        tool_label = str(tool_name or "").strip() or "unknown_tool"
        job_id = str(uuid4())
        job = AsyncToolJob(
            job_id=job_id,
            session_id=final_session_id,
            client_id=final_client_id,
            tool_name=tool_label,
            status="accepted",
            submitted_at_unix_ms=_now_ms(),
        )
        self._jobs[job_id] = job
        try:
            loop = asyncio.get_running_loop()
        except RuntimeError as exc:
            # 提交失败时显式关闭协程，避免 RuntimeWarning: coroutine was never awaited。
            coroutine_obj.close()
            raise RuntimeError("当前无运行中的事件循环，无法提交异步工具任务。") from exc
        self._tasks[job_id] = loop.create_task(self._run_job(job_id=job_id, coroutine_obj=coroutine_obj))
        return replace(job)

    def get_job(self, job_id: str) -> AsyncToolJob | None:
        """按 `job_id` 查询任务快照。

        逻辑：
        1. 从内存字典读取任务；
        2. 存在则返回副本，不存在返回 `None`。
        """
        job = self._jobs.get(job_id)
        if job is None:
            return None
        return replace(job)

    async def _run_job(self, *, job_id: str, coroutine_obj: Any) -> None:
        """执行后台协程并写入终态。

        逻辑：
        1. 标记 `running` 并记录开始时间；
        2. `await` 协程对象，成功记 `succeeded`；
        3. 捕获取消/异常并记 `cancelled` 或 `failed`；
        4. 完成后触发回调，供外层做自动推送。

        异常说明：
        - 任务内部异常被转换为状态字段，不向调用方抛出；
        - `CancelledError` 不再向上抛，避免污染业务调用链。
        """
        job = self._jobs[job_id]
        job.status = "running"
        job.started_at_unix_ms = _now_ms()
        try:
            result = await coroutine_obj
            job.status = "succeeded"
            job.result_text = str(result)
        except asyncio.CancelledError:
            job.status = "cancelled"
            job.error_text = "任务被取消。"
        except Exception as exc:  # noqa: BLE001
            job.status = "failed"
            job.error_text = str(exc)
        finally:
            job.finished_at_unix_ms = _now_ms()
            await self._notify_completed(job_id=job_id)

    async def _notify_completed(self, *, job_id: str) -> None:
        """在任务终态时触发回调列表。

        逻辑：
        1. 读取任务当前快照；
        2. 先向 message_queue 发送 `async_tool_result` 事件；
        3. 对每个完成回调依次执行；
        4. 回调若是协程则等待完成，避免静默丢失。

        关键边界：
        - 回调异常会被吞掉，确保不影响主任务生命周期；
        - 非终态调用直接返回。
        """
        job = self._jobs.get(job_id)
        if job is None or job.status not in self.TERMINAL_STATUSES:
            return
        snapshot = replace(job)
        await self._send_async_tool_result_to_message_queue(snapshot)
        for callback in list(self._completion_callbacks):
            try:
                maybe_awaitable = callback(snapshot)
                if inspect.isawaitable(maybe_awaitable):
                    await maybe_awaitable
            except Exception:
                # 回调是附加能力（如推送/埋点），失败不影响任务主流程。
                continue

    async def _send_async_tool_result_to_message_queue(self, job: AsyncToolJob) -> None:
        """将终态任务包装为 `async_tool_result` 并投递到 message_queue。

        逻辑：
        1. 若未注册发送器则直接返回；
        2. 组装包含任务与工具关键信息的 payload；
        3. 调用发送器投递到对应 session 队列；若返回 awaitable 则等待完成。

        关键边界：
        - 发送异常会被吞掉，避免影响任务完成主流程；
        - 仅终态任务会进入该方法（由 `_notify_completed` 保证）。
        """
        if self._message_queue_sender is None:
            return
        payload = {
            "event_type": "async_tool_result",
            "job_id": job.job_id,
            "session_id": job.session_id,
            "client_id": job.client_id,
            "tool_name": job.tool_name,
            "status": job.status,
            "result_text": job.result_text,
            "error_text": job.error_text,
            "submitted_at_unix_ms": job.submitted_at_unix_ms,
            "started_at_unix_ms": job.started_at_unix_ms,
            "finished_at_unix_ms": job.finished_at_unix_ms,
        }
        try:
            maybe = self._message_queue_sender(job.session_id, payload)
            if inspect.isawaitable(maybe):
                await maybe
        except Exception:
            return


_ASYNC_TOOL_RESULT_STORE = AsyncToolResultStore()


def get_async_tool_result_store() -> AsyncToolResultStore:
    """返回进程级异步工具结果仓库单例。"""
    return _ASYNC_TOOL_RESULT_STORE
