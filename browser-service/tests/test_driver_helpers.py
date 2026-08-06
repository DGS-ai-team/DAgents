from dagents_browser.agent_prompt import build_extend_system_message
from dagents_browser.ports import allocate_debug_port
from dagents_browser.task_result import summarize_agent_history, task_status_response


def normalize_max_elements(n: int) -> int:
    if n <= 0:
        return 150
    return min(n, 500)


def test_normalize_max_elements():
    assert normalize_max_elements(0) == 150
    assert normalize_max_elements(200) == 200
    assert normalize_max_elements(999) == 500


def test_allocate_debug_port_unique():
    p1 = allocate_debug_port("agt-a-browser", base_port=9222)
    p2 = allocate_debug_port("agt-b-browser", base_port=9222, used_ports={p1})
    assert p1 != p2
    assert 9222 <= p1 < 9222 + 200
    assert 9222 <= p2 < 9222 + 200


def test_allocate_debug_port_attach_mode():
    assert allocate_debug_port("any", base_port=9333, cdp_url="http://127.0.0.1:9333") == 9333


def test_extend_system_message_contains_dagents_rules():
    msg = build_extend_system_message(fs_root="/tmp/runtime", allowed_url_schemes=["https", "http"])
    assert "dagents_companion_rules" in msg
    assert "简体中文" in msg
    assert "/tmp/runtime" in msg
    assert "不要尝试登录" in msg
    assert "done" in msg
    assert "https" in msg
    assert "file://" in msg


class _FakeHistory:
    def final_result(self):
        return "页面标题是 Example Domain"

    def is_successful(self):
        return True

    def is_done(self):
        return True

    def number_of_steps(self):
        return 4

    def urls(self):
        return ["https://example.com", "https://example.com/"]

    def screenshot_paths(self):
        return ["/tmp/shot1.png"]

    def action_names(self):
        return ["navigate", "extract", "done"]

    def errors(self):
        return [None, None, None]

    def has_errors(self):
        return False

    def total_duration_seconds(self):
        return 12.5

    def extracted_content(self):
        return ["页面标题是 Example Domain"]


def test_summarize_agent_history():
    out = summarize_agent_history(_FakeHistory())
    assert out["summary"] == "页面标题是 Example Domain"
    assert out["success"] is True
    assert out["steps"] == 4
    assert out["last_url"] == "https://example.com/"
    assert out["last_screenshot_path"] == "/tmp/shot1.png"
    assert out["action_names"] == ["navigate", "extract", "done"]
    assert out["duration_seconds"] == 12.5


def test_task_status_response_flattens_result():
    entry = {
        "task_id": "btask-1",
        "session_key": "agt-1-browser",
        "task": "打开 example.com",
        "status": "completed",
        "max_steps": 50,
        "created_at": 1.0,
        "updated_at": 2.0,
        "error": None,
        "result": summarize_agent_history(_FakeHistory()),
    }
    resp = task_status_response(entry)
    assert resp["ok"] is True
    assert resp["url"] == "https://example.com/"
    assert resp["screenshot_path"] == "/tmp/shot1.png"
    detail = resp["detail"]
    assert detail["status"] == "completed"
    assert detail["summary"] == "页面标题是 Example Domain"
    assert detail["extracted_content"] == "页面标题是 Example Domain"
    assert detail["success"] is True
    assert "result" not in detail


def test_archive_and_recent(tmp_path):
    from dagents_browser.task_archive import (
        archive_task,
        format_recent_tasks_for_prompt,
        load_recent_tasks,
    )
    agent_fs = tmp_path / "fs"
    meta = archive_task(
        agent_fs=agent_fs,
        task_id="btask-a",
        task="打开 example.com",
        status="completed",
        result={"summary": "标题 Example", "success": True, "steps": 2, "action_names": ["navigate", "done"], "urls": ["https://example.com"]},
        session_key="agt-1-browser",
    )
    assert meta["detail_md"].endswith("btask-a.md")
    assert (agent_fs / "tasks" / "btask-a.md").is_file()
    recent = load_recent_tasks(agent_fs)
    assert len(recent) == 1
    block = format_recent_tasks_for_prompt(recent)
    assert "btask-a" in block
    assert "read_file" in block or "详情" in block
    assert "标题 Example" in block


def test_extend_includes_recent_block():
    from dagents_browser.agent_prompt import build_extend_system_message
    msg = build_extend_system_message(
        fs_root="/tmp/r",
        recent_tasks_block="<recent_browser_tasks>\n1. [btask-x]\n</recent_browser_tasks>",
    )
    assert "btask-x" in msg
    assert "tasks/<task_id>.md" in msg
