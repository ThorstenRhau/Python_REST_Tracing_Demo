"""
FastAPI + OpenTelemetry distributed tracing demo.

Demo endpoints:
    GET /orders/42      happy path (three sequential stages, then an outbound HTTP call)
    GET /orders/fail    error path (records exception, sets ERROR status, returns 500)

OTel is configured manually below — no `opentelemetry-instrument` wrapper —
so the entire setup is visible in this single file.
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

# ---- Domain/resource attributes (stable 3GPP/RAN identity lives here) ----
TELECOM_RESOURCE = {
    "service.name": "orders-api",
    # 3GPP/RAN topology — lowercase, dot namespaced, types per OTel spec
    "telecom.3gpp.gnb.id": "0x019a2b",
    "telecom.3gpp.gnb.name": "se-lkp-malmslaett",
    "telecom.3gpp.gnb.function.id": 17,
    "telecom.3gpp.gnb.function.name": "nr macro west",
    "telecom.3gpp.nr.cell.id": "0x4f12c7",
    "telecom.3gpp.nr.band": "n78",
    "telecom.3gpp.nr.pci": 123,
    "telecom.3gpp.plmn.mcc": "262",
    "telecom.3gpp.plmn.mnc": "01",
    "telecom.3gpp.slice.service.type": 1,
    "telecom.3gpp.slice.differentiator": "010203",
    "telecom.3gpp.amf.region": "eu-central-1",
    "telecom.3gpp.amf.set": "amf-set-07",
    "telecom.3gpp.smf.id": "smf-35",
    "telecom.3gpp.ue.imei": "356938035643809",
    "telecom.3gpp.ue.supi": "imsi-262019876543210",
    "telecom.o-ran.near_rt_ric.id": "ric-210-ne",
    "telecom.o-ran.o_du.id": "odu-201",
    "telecom.o-ran.o_cu.id": "ocu-504",
}


def configure_opentelemetry():
    """Wire up TracerProvider, OTLP gRPC exporter, and auto-instrumentation.

    For demo purposes clear-text telemetry is used; in production OTel and
    OTel components support mTLS.
    """
    provider = TracerProvider(resource=Resource.create(TELECOM_RESOURCE))
    otlp_exporter = OTLPSpanExporter(endpoint="localhost:4317", insecure=True)
    provider.add_span_processor(BatchSpanProcessor(otlp_exporter))
    trace.set_tracer_provider(provider)
    return trace.get_tracer(__name__)


tracer = configure_opentelemetry()

# --- FastAPI app + auto instrumentation ---
app = FastAPI()
FastAPIInstrumentor.instrument_app(app)  # server spans for inbound requests
HTTPXClientInstrumentor().instrument()   # client spans for outbound HTTP


# ── Stage 1: persistence ──────────────────────────────────────────────
async def db_lookup_order(order_id: str) -> None:
    """Look up the order in the database, with a Redis cache child span."""
    with tracer.start_as_current_span("db.lookup.order") as span:
        span.set_attribute("db.system", "sqlite")
        span.set_attribute("db.statement", "SELECT * FROM orders WHERE id = ?")
        span.set_attribute("telecom.3gpp.slice.service.type", 1)
        span.add_event("db.lookup.start", {"order.id": order_id})
        await asyncio.sleep(0.08)

        with tracer.start_as_current_span("cache.lookup") as cache_span:
            cache_span.set_attribute("cache.hit", True)
            cache_span.set_attribute("cache.system", "redis")
            cache_span.add_event("cache.fetch", {"key": f"order:{order_id}"})
            await asyncio.sleep(0.025)

        span.add_event("db.lookup.end", {"row.count": 1})


# ── Stage 2: radio context (3GPP / O-RAN) ────────────────────────────
async def enrich_with_radio_context(order_id: str) -> None:
    """Attach 3GPP and O-RAN context spans to the trace."""
    with tracer.start_as_current_span("radio.context.enrichment") as span:
        span.set_attribute("telecom.3gpp.ue.supi", f"imsi-26201{order_id:0>6}")
        span.set_attribute("telecom.3gpp.qos.flow.5qi", 9)
        span.set_attribute("telecom.3gpp.qos.slice.sd", "010203")
        await asyncio.gather(
            fetch_near_rt_ric_policy(order_id),
            check_o_du_health(),
        )
        span.add_event("radio.context.complete")


async def fetch_near_rt_ric_policy(order_id: str) -> None:
    with tracer.start_as_current_span("oran.near_rt_ric.policy") as span:
        span.set_attribute("telecom.o-ran.near_rt_ric.id", "ric-210-ne")
        span.set_attribute("telecom.o-ran.policy.id", "policy-5qi9-lowlat")
        span.add_event(
            "o-ran.policy.apply",
            {"order.id": order_id, "telecom.o-ran.ran.slice": "cot-slice-09"},
        )
        await asyncio.sleep(0.06)


async def check_o_du_health() -> None:
    with tracer.start_as_current_span("oran.odu.health") as span:
        span.set_attribute("telecom.o-ran.o_du.id", "odu-201")
        span.set_attribute("telecom.o-ran.o_du.state", "active")
        span.add_event("o-ran.o-du.heartbeat", {"latency.ms": 2.3})
        await asyncio.sleep(0.04)


# ── Stage 3: fulfilment ──────────────────────────────────────────────
async def fulfilment_checks(order_id: str) -> None:
    """Run inventory and charging checks in parallel under a parent span."""
    with tracer.start_as_current_span("fulfilment.checks") as span:
        span.set_attribute("app.order_id", order_id)
        await asyncio.gather(
            check_inventory(order_id),
            check_charging_session(order_id),
        )


async def check_inventory(order_id: str) -> None:
    with tracer.start_as_current_span("inventory.check") as span:
        span.set_attribute("app.inventory.region", "eu-central")
        span.add_event("inventory.lookup.start", {"order.id": order_id})

        # order_id == "fail" demonstrates the OTel error-recording pattern.
        if order_id == "fail":
            error_msg = "Inventory system unavailable for this ID"
            span.record_exception(RuntimeError(error_msg))
            span.set_status(Status(StatusCode.ERROR, error_msg))
            raise RuntimeError(error_msg)

        await asyncio.sleep(0.07)
        span.add_event("inventory.lookup.end", {"available": True})


async def check_charging_session(order_id: str) -> None:
    with tracer.start_as_current_span("charging.session.lookup") as span:
        span.set_attribute("telecom.3gpp.pcrf.id", "pcrf-12")
        span.set_attribute("telecom.3gpp.ccf.mode", "online")
        span.add_event("charging.session.start", {"order.id": order_id})
        await asyncio.sleep(0.055)
        span.add_event("charging.session.validated", {"quota.mb": 2048})


# ── HTTP entry point ─────────────────────────────────────────────────
@app.get("/orders/{order_id}")
async def get_order(order_id: str, request: Request):
    with tracer.start_as_current_span("order.load") as span:
        span.set_attribute("app.order_id", order_id)
        span.set_attribute("telecom.3gpp.session.id", f"pdu-{order_id:0>6}")

        # Print the trace_id so the presenter can locate this trace in otel-tui.
        trace_id_hex = format(span.get_span_context().trace_id, "032x")
        print(f"[demo] order_id={order_id} trace_id={trace_id_hex}")

        try:
            # Three stages run sequentially so the timeline narrates left-to-right.
            await db_lookup_order(order_id)
            await enrich_with_radio_context(order_id)
            await fulfilment_checks(order_id)
            span.add_event("order.load.complete")
        except RuntimeError as e:
            span.record_exception(e)
            span.set_status(Status(StatusCode.ERROR, str(e)))
            span.add_event("order.load.failed", {"reason": str(e)})
            raise HTTPException(status_code=500, detail=str(e))

    # Outbound call is traced automatically by HTTPX instrumentation.
    async with httpx.AsyncClient() as client:
        r = await client.get("https://httpbin.org/delay/1")
        upstream_ms = r.elapsed.total_seconds() * 1000

    # Enrich the active *server* span with request-scoped attributes.
    current = trace.get_current_span()
    client_ip = request.client.host if request.client else ""

    current.set_attribute("enduser.id", "u-123")
    current.set_attribute("client.address", client_ip)
    current.set_attribute("app.order_id", order_id)
    current.set_attribute("telecom.3gpp.qos.flow.5qi", 9)
    current.set_attribute("telecom.o-ran.o_du.state", "active")
    current.set_attribute("app.upstream_ms", upstream_ms)

    return {"id": order_id, "status": "ok", "upstream_ms": upstream_ms}
