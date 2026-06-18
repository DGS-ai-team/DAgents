from app.cli.render import sanitize_inline_tool_arg


def test_sanitize_inline_tool_arg_collapses_newlines() -> None:
    assert sanitize_inline_tool_arg("line1\nline2\r\nline3") == "line1 line2 line3"


def test_sanitize_inline_tool_arg_strips_edges() -> None:
    assert sanitize_inline_tool_arg("  hello  ") == "hello"
