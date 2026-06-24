from app.cli.tool_calls_streaming import streaming_tool_call_preview


def test_streaming_bash_command_partial() -> None:
    raw = '{"command": "ls -la /tmp'
    args, code, lexer = streaming_tool_call_preview("bash_run", raw)
    assert args.get("command") == "ls -la /tmp"
    assert code == "ls -la /tmp"
    assert lexer == "bash"


def test_streaming_invalid_json_fallback() -> None:
    raw = '{"path": "/etc/pass'
    args, code, lexer = streaming_tool_call_preview("read_file", raw)
    # read_file intentionally does not support partial argument extraction
    # for invalid/incomplete JSON and must use the raw JSON fallback.
    assert args.get("path") is None
    assert args == {}
    assert code == raw
    assert lexer == "json"


def test_streaming_complete_json_uses_parsed() -> None:
    raw = '{"command": "echo hi"}'
    args, code, lexer = streaming_tool_call_preview("bash_run", raw)
    assert args.get("command") == "echo hi"
    assert code is None
    assert lexer == "bash"


def test_streaming_search_replace_partial_no_raw_json() -> None:
    raw = '{"path": "data/count.py", "old_string": "SELECT *'
    args, code, lexer = streaming_tool_call_preview("search_replace", raw)
    assert args.get("path") == "data/count.py"
    assert args.get("old_string") is None
    assert code is None
    assert lexer == "text"
