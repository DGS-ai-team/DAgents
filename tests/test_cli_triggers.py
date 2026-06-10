from app.cli.triggers_format import format_condition_summary, format_triggers_panel, format_unix_timestamp


def test_format_condition_summary_interval() -> None:
    assert format_condition_summary({"interval_seconds": 300}) == "interval 300s"


def test_format_condition_summary_schedule() -> None:
    assert format_condition_summary({"schedule": {"kind": "daily", "hour": 9}}) == "schedule:daily"


def test_format_unix_timestamp_zero() -> None:
    assert format_unix_timestamp(None) == "-"


def test_format_triggers_panel_empty() -> None:
    panel = format_triggers_panel([])
    assert panel is not None


def test_format_triggers_panel_one_item() -> None:
    panel = format_triggers_panel(
        [
            {
                "trigger_id": "t-1",
                "name": "喝水提醒",
                "condition": {"interval_seconds": 3600},
                "enabled": True,
                "fire_count": 2,
                "task_template": "提醒用户喝水",
            }
        ]
    )
    assert panel is not None
