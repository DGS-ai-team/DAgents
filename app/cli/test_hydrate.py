from types import SimpleNamespace

from app.cli.hydrate import apply_hydrate_turn_state, transcript_updates_from_hydrate

from app.cli.render import TranscriptKind


def test_apply_hydrate_turn_state_a2a_relay() -> None:
    controller = SimpleNamespace(
        _awaiting_user_turn=True,
        _user_turn_started=True,
        _user_turn_done=__import__("asyncio").Event(),
    )
    controller._user_turn_done.set()
    apply_hydrate_turn_state(
        controller,
        {
            "run_turn_phase": "awaiting_hitl",
            "pending_a2a_relay": {
                "event_type": "approval_required",
                "data": {"a2a_relay": True, "a2a_task_id": "task-1"},
            },
        },
    )
    assert controller._awaiting_user_turn is False


def test_transcript_updates_from_hydrate_user_and_assistant() -> None:
    updates = transcript_updates_from_hydrate(
        [
            {"kind": "user", "text": "hello"},
            {"kind": "assistant", "text": "hi"},
        ],
        show_reasoning=False,
    )
    assert len(updates) == 3
    assert updates[0].kind == TranscriptKind.LINE
    assert updates[0].text == "hello"
