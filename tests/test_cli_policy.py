from app.cli.tui.policy_view import PolicyViewState, entry_mode, policy_mode_label


def test_policy_mode_label() -> None:
    assert policy_mode_label("never") == "自动允许"
    assert policy_mode_label("always") == "需审批"
    assert policy_mode_label("rule") == "特殊规则"
    assert policy_mode_label("deny") == "禁止"


def test_entry_mode_prefers_mode_field() -> None:
    assert entry_mode({"mode": "rule", "decision": "require_approval"}) == "rule"
    assert entry_mode({"decision": "allow_auto"}) == "never"


def test_policy_visible_rows_tools_filter() -> None:
    state = PolicyViewState()
    state.mode = True
    state.snapshot = {
        "tools": [
            {"name": "read_file", "mode": "never"},
            {"name": "write_file", "mode": "always"},
        ],
        "shell": {},
    }
    state.filter_text = "read"
    rows = state.visible_rows()
    assert len(rows) == 1
    assert rows[0]["tool_name"] == "read_file"
    assert rows[0]["mode"] == "never"


def test_policy_shell_lists_all_configured_entries() -> None:
    state = PolicyViewState()
    state.mode = True
    state.tab = "shell"
    state.shell_type = "bash"
    state.snapshot = {
        "tools": [],
        "shell": {
            "bash": [
                {"command": "ls", "mode": "never"},
                {"command": "git", "mode": "always"},
                {"command": "rm", "mode": "deny"},
            ],
        },
    }
    rows = state.visible_rows()
    names = {row["command"] for row in rows}
    assert names == {"ls", "git", "rm"}


def test_policy_render_text_paginates_by_viewport() -> None:
    state = PolicyViewState()
    state.mode = True
    state.tab = "shell"
    state.shell_type = "bash"
    state.snapshot = {
        "tools": [],
        "shell": {
            "bash": [
                {"command": f"cmd{i:02d}", "mode": "never"}
                for i in range(40)
            ],
        },
    }
    text = state.render_text(viewport_rows=12)
    row_lines = [line for line in text.splitlines() if line.startswith("  ") or line.startswith("> ")]
    assert len(row_lines) < 40
    assert "显示 1-" in text
    assert "/ 40" in text

    state.cursor = 25
    text_page2 = state.render_text(viewport_rows=12)
    assert "> cmd25" in text_page2
    assert "显示" in text_page2


def test_policy_remove_local_shell_entry() -> None:
    state = PolicyViewState()
    state.tab = "shell"
    state.shell_type = "bash"
    state.snapshot = {"shell": {"bash": [{"command": "ls", "mode": "never"}]}}
    state.remove_local_shell_entry(command="ls")
    assert state.visible_rows() == []
