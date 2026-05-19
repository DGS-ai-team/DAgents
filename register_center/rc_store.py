"""Register Center 的内存存储实现。"""

from __future__ import annotations

import threading
import time

from rc_models import AgentRecord, AgentUpsertRequest


class AgentRegistryStore:
    """Agent 登记信息的内存仓库。

    逻辑：
    1. 使用字典按 `agent_id` 保存当前生效记录；
    2. 所有写操作持有互斥锁，避免并发请求下出现覆盖竞态；
    3. 查询时返回快照副本，避免外部误改内部状态。

    关键分支/边界：
    - 相同 `agent_id` 的重复 upsert 采用覆盖策略（last-write-wins）；
    - 不存在的 `agent_id` 删除返回 False，由上层转换为 404；
    - 列表查询支持按分组“成员关系”过滤（记录分组列表包含该分组即命中）。

    与外部交互：
    - 无网络/文件/数据库交互，仅依赖进程内内存和系统时间。

    异常说明：
    - 本类不主动吞异常，原则上仅抛出编程期异常。

    副作用说明：
    - 会修改内部 `_records` 状态。
    """

    def __init__(self) -> None:
        """初始化内存仓库。

        逻辑：
        1. 创建可重入锁；
        2. 初始化空记录字典。

        关键分支/边界：
        - 无。

        与外部交互：
        - 无。

        异常说明：
        - 无显式异常处理。

        副作用说明：
        - 创建实例级内存状态。
        """

        self._lock = threading.RLock()
        self._records: dict[str, AgentRecord] = {}

    def upsert(self, payload: AgentUpsertRequest) -> AgentRecord:
        """写入或覆盖一条 Agent 记录。

        逻辑：
        1. 取当前 Unix 秒时间作为 `registered_at_unix`；
        2. 构造标准化 `AgentRecord`；
        3. 在锁内按 `agent_id` 覆盖写入并返回写入结果。

        关键分支/边界：
        - 相同 `agent_id` 总是覆盖旧值，不做冲突报错。

        与外部交互：
        - 读取系统时间（`time.time()`）。

        异常说明：
        - 不捕获异常，向上抛出。

        副作用说明：
        - 更新 `_records` 中对应键的值。
        """

        now_unix = int(time.time())
        record = AgentRecord(
            agent_id=payload.agent_id,
            base_url=payload.base_url,
            discovery_group=payload.discovery_group,
            capabilities_hint=payload.capabilities_hint,
            registered_at_unix=now_unix,
            expires_at_unix=now_unix + int(payload.ttl_seconds),
        )
        with self._lock:
            self._records[payload.agent_id] = record
            return self._records[payload.agent_id]

    def get(self, agent_id: str) -> AgentRecord | None:
        """按 agent_id 查询单条记录。

        逻辑：
        1. 在锁内读取字典；
        2. 找不到时返回 `None`，由上层路由决定 HTTP 语义。

        关键分支/边界：
        - 不存在时返回空，不抛异常。

        与外部交互：
        - 无。

        异常说明：
        - 不捕获异常，向上抛出。

        副作用说明：
        - 无。
        """

        with self._lock:
            self._prune_expired_locked()
            return self._records.get(agent_id)

    def list(self, discovery_group: str | None = None) -> list[AgentRecord]:
        """按条件列出记录快照。

        逻辑：
        1. 在锁内读取当前全部记录；
        2. 若提供 `discovery_group`，按成员关系过滤；
        3. 以 `agent_id` 排序，保证返回顺序稳定。

        关键分支/边界：
        - `discovery_group=None` 表示不过滤；
        - 记录挂载多个分组时，只要包含目标分组即返回。

        与外部交互：
        - 无。

        异常说明：
        - 不捕获异常，向上抛出。

        副作用说明：
        - 无。
        """

        with self._lock:
            self._prune_expired_locked()
            records = list(self._records.values())
            if discovery_group is not None:
                # 多分组场景下，命中任一登记分组即视为可见。
                records = [item for item in records if discovery_group in item.discovery_group]
            return sorted(records, key=lambda item: item.agent_id)

    def _prune_expired_locked(self) -> None:
        """清理已超过 TTL 的登记记录。

        逻辑：
        1. 读取当前 Unix 秒；
        2. 找出 `expires_at_unix <= now` 的记录；
        3. 在调用方已持锁的前提下删除。

        关键边界：
        - 本方法要求调用方持有 `_lock`，避免重复加锁造成语义混乱。
        """
        now_unix = int(time.time())
        expired = [agent_id for agent_id, item in self._records.items() if item.expires_at_unix <= now_unix]
        for agent_id in expired:
            self._records.pop(agent_id, None)

    def delete(self, agent_id: str) -> bool:
        """删除指定 Agent 记录。

        逻辑：
        1. 在锁内检查 `agent_id` 是否存在；
        2. 存在则删除并返回 `True`；
        3. 不存在返回 `False`。

        关键分支/边界：
        - 删除不存在记录时不抛异常，由上层转 404。

        与外部交互：
        - 无。

        异常说明：
        - 不捕获异常，向上抛出。

        副作用说明：
        - 可能减少 `_records` 中条目数量。
        """

        with self._lock:
            if agent_id not in self._records:
                return False
            del self._records[agent_id]
            return True

    def count(self) -> int:
        """返回当前记录总数。

        逻辑：
        1. 在锁内读取字典长度；
        2. 返回整数结果。

        关键分支/边界：
        - 无。

        与外部交互：
        - 无。

        异常说明：
        - 不捕获异常，向上抛出。

        副作用说明：
        - 无。
        """

        with self._lock:
            return len(self._records)
