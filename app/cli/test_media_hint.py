from app.cli.media_hint import media_hint_lines, user_media_hint_lines


def test_media_hint_lines_from_media_array() -> None:
    lines = media_hint_lines(
        {
            "tool_name": "browser_snapshot",
            "media": [{"url": "/v1/sessions/s1/media/med_1", "label": "browser_snapshot"}],
        }
    )
    assert lines == ["browser_snapshot: /v1/sessions/s1/media/med_1"]


def test_media_hint_lines_show_image_path() -> None:
    lines = media_hint_lines(
        {
            "tool_name": "show_image",
            "content": "[SHOW_IMAGE]\npath=reports/chart.png\nstatus=ok",
        }
    )
    assert lines == ["image path: reports/chart.png"]


def test_user_media_hint_lines() -> None:
    lines = user_media_hint_lines({"media": [{"url": "/v1/sessions/s/media/med_u", "label": "user_upload"}]})
    assert len(lines) == 1
