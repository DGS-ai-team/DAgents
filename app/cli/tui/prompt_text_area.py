from __future__ import annotations

from textual import events
from textual.widgets import TextArea


class PromptTextArea(TextArea):
    """聊天输入区：Enter 提交，Shift+Enter 换行。"""

    async def _on_key(self, event: events.Key) -> None:
        """拦截输入框快捷键并委托给 App。

        逻辑：
        1. ``enter``：阻止默认换行，委托 App.submit_prompt；
        2. ``escape``：阻止 TextArea 吃掉事件，委托 App 取消当前 turn；
        3. ``shift+enter`` 及其他：走 TextArea 默认键处理。

        关键边界：
        - TextArea 聚焦时 App.on_key 可能收不到 Esc，因此这里必须显式转发。
        """
        if self.read_only:
            return
        if event.key == "enter":
            event.stop()
            event.prevent_default()
            app = self.app
            submit = getattr(app, "submit_prompt", None)
            if submit is not None:
                await submit()
            return
        if event.key == "escape":
            event.stop()
            event.prevent_default()
            app = self.app
            if getattr(app, "_context_mode", False):
                exit_ctx = getattr(app, "_exit_context_view", None)
                if callable(exit_ctx):
                    exit_ctx()
                return
            if getattr(app, "_policy_view", None) is not None and app._policy_view.mode:
                exit_policy = getattr(app, "_exit_policy_view", None)
                if callable(exit_policy):
                    exit_policy()
                return
            cancel = getattr(app, "_cancel_current_turn", None)
            if callable(cancel):
                cancel()
            return
        await super()._on_key(event)

    def on_text_area_changed(self, event: TextArea.Changed) -> None:
        app = self.app
        notify = getattr(app, "_on_policy_filter_changed", None)
        if callable(notify):
            notify()
