from __future__ import annotations

from textual.app import ComposeResult
from textual.containers import Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, Input, Label, Static

from app.cli.approval import (
    ApprovalDecision,
    ToolApprovalRequest,
    build_all_approved_decision,
    build_all_rejected_decision,
    build_selection_decision,
    parse_selection_tokens,
)
from app.cli.render import tool_summary


class ApprovalScreen(ModalScreen[ApprovalDecision]):
    """工具审批弹窗：全批准 / 全拒绝 / 按编号选择。"""

    DEFAULT_CSS = """
    ApprovalScreen {
        align: center middle;
    }
    #approval-dialog {
        width: 90%;
        max-width: 100;
        height: auto;
        max-height: 90%;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }
    #approval-details {
        height: auto;
        max-height: 20;
        overflow-y: auto;
        margin: 1 0;
    }
    #approval-select-row {
        height: auto;
        margin-top: 1;
    }
  """

    def __init__(self, requests: list[ToolApprovalRequest]) -> None:
        super().__init__()
        self._requests = requests

    def compose(self) -> ComposeResult:
        with Vertical(id="approval-dialog"):
            yield Label("Tool approval required", id="approval-title")
            yield Static(self._details_text(), id="approval-details")
            yield Button("Approve all (a)", id="approve-all", variant="success")
            yield Button("Reject all (r)", id="reject-all", variant="error")
            with Vertical(id="approval-select-row"):
                yield Label("Select by number or call ID (space-separated):")
                yield Input(placeholder="e.g. 1 call_abc", id="selection-input")
                yield Button("Apply selection (s)", id="apply-selection", variant="primary")

    def _details_text(self) -> str:
        if not self._requests:
            return "No tool calls provided; rejecting by default."
        return "\n".join(tool_summary(item, index) for index, item in enumerate(self._requests, start=1))

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "approve-all":
            self.dismiss(build_all_approved_decision(self._requests))
        elif event.button.id == "reject-all":
            self.dismiss(build_all_rejected_decision(self._requests))
        elif event.button.id == "apply-selection":
            self._apply_selection()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "selection-input":
            self._apply_selection()

    def _apply_selection(self) -> None:
        selection_input = self.query_one("#selection-input", Input)
        try:
            approved_ids = parse_selection_tokens(selection_input.value.strip(), self._requests)
        except ValueError as exc:
            self.notify(str(exc), severity="error")
            return
        self.dismiss(build_selection_decision(self._requests, approved_ids))
