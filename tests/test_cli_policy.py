from app.cli.tui.policy_view import PolicyViewState, policy_decision_label


def test_policy_decision_label() -> None:
    assert policy_decision_label("allow_auto") == "白名单"
    assert policy_decision_label("deny") == "黑名单"
    assert policy_decision_label("require_approval") == "需审批"


def test_policy_visible_rows_tools_filter() -> None:
    state = PolicyViewState()
    state.mode = True
    state.snapshot = {
        "tools": [
            {"name": "read_file", "decision": "allow_auto"},
            {"name": "write_file", "decision": "require_approval"},
        ],
        "shell": {},
    }
    state.filter_text = "read"
    rows = state.visible_rows()
    assert len(rows) == 1
    assert rows[0]["tool_name"] == "read_file"


def test_policy_shell_default_filter() -> None:
    state = PolicyViewState()
    state.mode = True
    state.tab = "shell"
    state.shell_type = "bash"
    state.snapshot = {
        "tools": [],
        "shell": {
            "bash": [
                {"command": "ls", "decision": "allow_auto"},
                {"command": "git", "decision": "require_approval"},
                {"command": "rm", "decision": "deny"},
            ],
        },
    }
    rows = state.visible_rows()
    names = {row["command"] for row in rows}
    assert names == {"ls", "rm"}
    state.shell_show_all = True
    rows_all = state.visible_rows()
    assert len(rows_all) == 3


def test_policy_render_text_paginates_by_viewport() -> None:
    state = PolicyViewState()
    state.mode = True
    state.tab = "shell"
    state.shell_type = "bash"
    state.snapshot = {
        "tools": [],
        "shell": {
            "bash": [
                {"command": f"cmd{i:02d}", "decision": "allow_auto"}
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
