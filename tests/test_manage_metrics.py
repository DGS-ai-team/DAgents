from __future__ import annotations

import unittest

from manage.platform.metrics import (
    metrics_text,
    record_workgroup_timeline_event,
    record_workgroup_ws_event,
)


class ManageMetricsTests(unittest.TestCase):
    def test_workgroup_metrics_use_bounded_labels(self) -> None:
        record_workgroup_ws_event(direction="inbound", event="session.hello")
        record_workgroup_ws_event(direction="inbound", event="untrusted-event")
        record_workgroup_timeline_event("human_message")
        record_workgroup_timeline_event("untrusted-event")

        body, _ = metrics_text()
        text = body.decode("utf-8")
        self.assertIn(
            'dagents_manage_workgroup_ws_events_total{direction="inbound",event="session.hello"}',
            text,
        )
        self.assertIn(
            'dagents_manage_workgroup_ws_events_total{direction="inbound",event="other"}',
            text,
        )
        self.assertIn(
            'dagents_manage_workgroup_timeline_events_total{event_type="human_message"}',
            text,
        )
        self.assertIn(
            'dagents_manage_workgroup_timeline_events_total{event_type="other"}',
            text,
        )
