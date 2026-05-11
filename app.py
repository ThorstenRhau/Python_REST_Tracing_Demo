"""
FastAPI + OpenTelemetry distributed tracing demo.

Demo endpoints:
    POST /slice-sessions/session-42/activate    happy path
    POST /slice-sessions/deny/activate          clean policy-denial path

OTel is configured manually below, without the `opentelemetry-instrument`
wrapper, so the setup stays visible in this single file.
"""

import asyncio

import httpx
from fastapi import FastAPI, HTTPException, Request

# --- OpenTelemetry setup (essentials) ---
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace import Status, StatusCode


PROVISIONING_URL = "http://127.0.0.1:8000/provisioning/slice-sessions"

# Stable service and topology attributes live on the resource. Subscriber,
# session, and policy facts are attached to spans because they vary by request.
TELECOM_RESOURCE = {
    "service.name": "slice-activation-api",
    "service.namespace": "otel-telecom-demo",
    "telecom.3gpp.gnb.id": "0x019a2b",
    "telecom.3gpp.gnb.name": "se-lkp-malmslaett",
    "telecom.3gpp.gnb.function.id": 17,
    "telecom.3gpp.gnb.function.name": "nr_macro_west",
    "telecom.3gpp.nr.cell.id": "0x4f12c7",
    "telecom.3gpp.nr.band": "n78",
    "telecom.3gpp.nr.pci": 123,
    "telecom.o_ran.near_rt_ric.id": "ric-210-ne",
    "telecom.o_ran.o_du.id": "odu-201",
    "telecom.o_ran.o_cu.id": "ocu-504",
}


def configure_opentelemetry():
    """Wire up TracerProvider, OTLP gRPC exporter, and auto-instrumentation."""
    provider = TracerProvider(resource=Resource.create(TELECOM_RESOURCE))
    otlp_exporter = OTLPSpanExporter(endpoint="localhost:4317", insecure=True)
    provider.add_span_processor(BatchSpanProcessor(otlp_exporter))
    trace.set_tracer_provider(provider)
    return trace.get_tracer(__name__)


tracer = configure_opentelemetry()

# --- FastAPI app + auto instrumentation ---
app = FastAPI()
FastAPIInstrumentor.instrument_app(app)  # server spans for inbound requests
HTTPXClientInstrumentor().instrument()  # client spans for outbound HTTP


async def validate_subscriber(session_id: str) -> dict:
    """Validate subscriber eligibility and attach privacy-safe subscriber context."""
    with tracer.start_as_current_span("subscriber.validate") as span:
        subscriber = {
            "supi_hash": "sha256:demo-26201-9876543210",
            "tenant": "consumer_mobile",
            "plmn_mcc": "262",
            "plmn_mnc": "01",
        }
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("user.hash", subscriber["supi_hash"])
        span.set_attribute("network.carrier.mcc", subscriber["plmn_mcc"])
        span.set_attribute("network.carrier.mnc", subscriber["plmn_mnc"])
        span.set_attribute("network.connection.type", "cell")
        span.set_attribute("network.connection.subtype", "nr")
        span.add_event("subscriber.eligibility.checked", {"eligible": True})
        await asyncio.sleep(0.04)
        return subscriber


async def resolve_slice_profile(session_id: str) -> dict:
    """Resolve the requested slice and QoS profile."""
    with tracer.start_as_current_span("slice.resolve_profile") as span:
        slice_profile = {
            "service_type": 1,
            "differentiator": "010203",
            "name": "enhanced_mobile_broadband",
            "five_qi": 9,
            "priority_level": 6,
        }
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("telecom.3gpp.slice.service_type", slice_profile["service_type"])
        span.set_attribute("telecom.3gpp.slice.differentiator", slice_profile["differentiator"])
        span.set_attribute("telecom.3gpp.qos.flow.5qi", slice_profile["five_qi"])
        span.set_attribute("telecom.3gpp.qos.priority_level", slice_profile["priority_level"])
        span.add_event("slice.profile.resolved", {"slice.name": slice_profile["name"]})
        await asyncio.sleep(0.05)
        return slice_profile


async def enrich_with_radio_context(session_id: str) -> None:
    """Attach 3GPP and O-RAN radio context to the trace."""
    with tracer.start_as_current_span("radio.context_enrichment") as span:
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("telecom.3gpp.nr.cell.id", "0x4f12c7")
        span.set_attribute("telecom.3gpp.nr.band", "n78")
        span.set_attribute("telecom.3gpp.nr.pci", 123)
        await asyncio.gather(
            fetch_near_rt_ric_policy(session_id),
            check_o_du_health(),
        )
        span.add_event("radio.context.complete")


async def fetch_near_rt_ric_policy(session_id: str) -> None:
    with tracer.start_as_current_span("ric.policy_fetch") as span:
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("telecom.o_ran.near_rt_ric.id", "ric-210-ne")
        span.set_attribute("telecom.o_ran.policy.id", "policy-embb-default")
        span.add_event("ric.policy.loaded", {"policy.version": "2026.05.11"})
        await asyncio.sleep(0.04)


async def check_o_du_health() -> None:
    with tracer.start_as_current_span("odu.health_check") as span:
        span.set_attribute("telecom.o_ran.o_du.id", "odu-201")
        span.set_attribute("telecom.o_ran.o_du.state", "active")
        span.add_event("odu.heartbeat", {"latency.ms": 2.3})
        await asyncio.sleep(0.03)


async def evaluate_ric_admission(session_id: str, slice_profile: dict) -> bool:
    """Ask the near-RT RIC whether this slice session should be admitted."""
    with tracer.start_as_current_span("ric.admission_decision") as span:
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("telecom.o_ran.near_rt_ric.id", "ric-210-ne")
        span.set_attribute("telecom.o_ran.policy.id", "policy-embb-default")
        span.set_attribute("telecom.3gpp.slice.service_type", slice_profile["service_type"])
        span.set_attribute("telecom.3gpp.slice.differentiator", slice_profile["differentiator"])
        await asyncio.sleep(0.06)

        if session_id == "deny":
            reason = "RIC admission denied for requested slice"
            span.set_status(Status(StatusCode.ERROR, reason))
            span.add_event("ric.admission.denied", {"reason": "policy_capacity_guard"})
            return False

        span.add_event("ric.admission.accepted", {"decision.latency.ms": 14.2})
        return True


async def check_charging_quota(session_id: str, slice_profile: dict) -> None:
    with tracer.start_as_current_span("charging.quota_check") as span:
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("telecom.3gpp.chf.id", "chf-12")
        span.set_attribute("telecom.3gpp.charging.mode", "online")
        span.set_attribute("telecom.3gpp.slice.service_type", slice_profile["service_type"])
        span.add_event("charging.quota.validated", {"quota.mb": 2048})
        await asyncio.sleep(0.05)


async def commit_provisioning(session_id: str, slice_profile: dict) -> float:
    """Call a second instrumented endpoint so HTTP propagation is visible."""
    with tracer.start_as_current_span("provisioning.commit") as span:
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("telecom.3gpp.slice.service_type", slice_profile["service_type"])

        async with httpx.AsyncClient() as client:
            response = await client.post(
                f"{PROVISIONING_URL}/{session_id}",
                json={
                    "slice": {
                        "service_type": slice_profile["service_type"],
                        "differentiator": slice_profile["differentiator"],
                    }
                },
            )
            response.raise_for_status()

        upstream_ms = response.elapsed.total_seconds() * 1000
        span.set_attribute("app.provisioning_ms", upstream_ms)
        span.add_event("provisioning.committed", response.json())
        return upstream_ms


@app.post("/slice-sessions/{session_id}/activate")
async def activate_slice_session(session_id: str, request: Request):
    server_span = trace.get_current_span()
    client_ip = request.client.host if request.client else ""
    server_span.set_attribute("client.address", client_ip)
    server_span.set_attribute("app.session.id", session_id)

    denial_reason = ""
    with tracer.start_as_current_span("slice_session.activate") as span:
        span.set_attribute("app.session.id", session_id)

        trace_id_hex = format(span.get_span_context().trace_id, "032x")
        print(f"[demo] session_id={session_id} trace_id={trace_id_hex}")

        subscriber = await validate_subscriber(session_id)
        slice_profile = await resolve_slice_profile(session_id)
        await enrich_with_radio_context(session_id)

        admitted = await evaluate_ric_admission(session_id, slice_profile)
        if not admitted:
            denial_reason = "RIC admission denied for requested slice"
            span.set_status(Status(StatusCode.ERROR, denial_reason))
            span.add_event("slice_session.activation_denied", {"reason": denial_reason})
        else:
            await check_charging_quota(session_id, slice_profile)
            provisioning_ms = await commit_provisioning(session_id, slice_profile)
            span.add_event("slice_session.activated")

    if denial_reason:
        raise HTTPException(status_code=403, detail=denial_reason)

    server_span.set_attribute("user.hash", subscriber["supi_hash"])
    server_span.set_attribute("network.carrier.mcc", subscriber["plmn_mcc"])
    server_span.set_attribute("network.carrier.mnc", subscriber["plmn_mnc"])
    server_span.set_attribute("telecom.3gpp.slice.service_type", slice_profile["service_type"])
    server_span.set_attribute("telecom.3gpp.slice.differentiator", slice_profile["differentiator"])
    server_span.set_attribute("telecom.3gpp.qos.flow.5qi", slice_profile["five_qi"])
    server_span.set_attribute("app.provisioning_ms", provisioning_ms)

    return {
        "id": session_id,
        "status": "activated",
        "slice": {
            "service_type": slice_profile["service_type"],
            "differentiator": slice_profile["differentiator"],
            "qos_flow_5qi": slice_profile["five_qi"],
        },
        "provisioning_ms": provisioning_ms,
    }


@app.post("/provisioning/slice-sessions/{session_id}")
async def provision_slice_session(session_id: str, payload: dict):
    """Tiny downstream endpoint used to show HTTP context propagation."""
    with tracer.start_as_current_span("provisioning.write_model") as span:
        span.set_attribute("app.session.id", session_id)
        span.set_attribute("db.system.name", "sqlite")
        span.set_attribute("db.query.summary", "INSERT slice_session")
        span.set_attribute(
            "db.query.text",
            "INSERT INTO slice_sessions (id, sst, sd, status) VALUES (?, ?, ?, ?)",
        )
        span.set_attribute("telecom.3gpp.slice.service_type", payload["slice"]["service_type"])
        span.set_attribute("telecom.3gpp.slice.differentiator", payload["slice"]["differentiator"])
        span.add_event("provisioning.write.committed", {"status": "active"})
        await asyncio.sleep(0.08)

    return {"provisioning_status": "committed"}
