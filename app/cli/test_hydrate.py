from app.cli.hydrate import transcript_updates_from_hydrate
from app.cli.render import TranscriptKind


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
