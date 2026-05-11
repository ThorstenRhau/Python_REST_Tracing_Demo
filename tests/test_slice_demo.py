import datetime
import importlib
import sys
import unittest
from unittest.mock import patch

from fastapi.testclient import TestClient
from opentelemetry.sdk.trace.export import SpanExportResult


EXPORTED_SPANS = []


class CapturingExporter:
    def __init__(self, *args, **kwargs):
        pass

    def export(self, spans):
        EXPORTED_SPANS.extend(spans)
        return SpanExportResult.SUCCESS

    def shutdown(self):
        return None

    def force_flush(self, timeout_millis=30000):
        return True


class FakeProvisioningResponse:
    elapsed = datetime.timedelta(milliseconds=87)

    def raise_for_status(self):
        return None

    def json(self):
        return {"provisioning_status": "committed"}


class FakeAsyncClient:
    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc, tb):
        return False

    async def post(self, url, json):
        return FakeProvisioningResponse()


def load_demo_app():
    for module_name in list(sys.modules):
        if module_name == "app":
            del sys.modules[module_name]

    with patch(
        "opentelemetry.exporter.otlp.proto.grpc.trace_exporter.OTLPSpanExporter",
        CapturingExporter,
    ):
        demo = importlib.import_module("app")

    demo.httpx.AsyncClient = FakeAsyncClient
    return demo


class SliceDemoTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.demo = load_demo_app()
        cls.client = TestClient(cls.demo.app)

    def setUp(self):
        EXPORTED_SPANS.clear()

    def force_flush(self):
        self.demo.trace.get_tracer_provider().force_flush()

    def test_slice_activation_happy_path_uses_slice_narrative(self):
        response = self.client.post("/slice-sessions/session-42/activate")

        self.force_flush()

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["status"], "activated")
        self.assertEqual(response.json()["slice"]["service_type"], 1)

        span_names = {span.name for span in EXPORTED_SPANS}
        self.assertIn("slice_session.activate", span_names)
        self.assertIn("slice.resolve_profile", span_names)
        self.assertIn("ric.admission_decision", span_names)
        self.assertIn("charging.quota_check", span_names)
        self.assertIn("provisioning.commit", span_names)

    def test_policy_denial_is_a_clean_error_trace(self):
        response = self.client.post("/slice-sessions/deny/activate")

        self.force_flush()

        self.assertEqual(response.status_code, 403)
        self.assertIn("RIC admission denied", response.json()["detail"])

        spans = {span.name: span for span in EXPORTED_SPANS}
        self.assertEqual(spans["ric.admission_decision"].status.status_code.name, "ERROR")
        self.assertEqual(spans["slice_session.activate"].status.status_code.name, "ERROR")

        for span_name in ("ric.admission_decision", "slice_session.activate"):
            exception_events = [
                event for event in spans[span_name].events if event.name == "exception"
            ]
            self.assertEqual(exception_events, [])


if __name__ == "__main__":
    unittest.main()
