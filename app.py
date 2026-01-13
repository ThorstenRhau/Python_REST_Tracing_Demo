# app.py
from fastapi import FastAPI, Request, HTTPException
import httpx
import asyncio

# --- OpenTelemetry setup (essentials) ---
from opentelemetry import trace

# Status and StatusCode are used to explicitly mark spans as error
from opentelemetry.trace import Status, StatusCode
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
# If you want HTTP instead of gRPC, swap the import:
# from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

# ---- Domain/resource attributes (stable 3GPP/RAN identity lives here) ----
TELECOM_RESOURCE = {
    "service.name": "orders-api",
    # 3GPP/RAN topology — lowercase, dot namespaced, types per OTel spec
    "telecom.3gpp.gnb.id": "0x019a2b",  # hex -> string
    "telecom.3gpp.gnb.name": "se-lkp-malmslaett",  # normalized to lowercase/hyphen-safe
    "telecom.3gpp.gnb.function.id": 17,  # int
    "telecom.3gpp.gnb.function.name": "nr macro west",
    "telecom.3gpp.nr.cell.id": "0x019a2b",  # hex -> string
    "telecom.3gpp.nr.band": "n78",
    "telecom.3gpp.nr.pci": 123,  # int
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


# --- Configure provider and exporter ---
def configure_opentelemetry():
    """
    Sets up the OpenTelemetry TracerProvider and Exporter.
    This is illustrative: in production, this is often done via the
    opentelemetry-instrument agent, but manual configuration gives
    you fine-grained control.
    """
    provider = TracerProvider(resource=Resource.create(TELECOM_RESOURCE))

    # Export spans to OTLP gRPC on localhost (otel-tui default is 4317)
    # For demo purposes clear text telemetry is used, in production
    # OTel and OTel components support mTLS
    otlp_exporter = OTLPSpanExporter(endpoint="localhost:4317", insecure=True)
    provider.add_span_processor(BatchSpanProcessor(otlp_exporter))

    trace.set_tracer_provider(provider)
    return trace.get_tracer(__name__)


# Initialize tracer
tracer = configure_opentelemetry()

# --- FastAPI app + auto instrumentation ---
app = FastAPI()

# Auto-create spans for inbound requests (server spans)
# Note: If running with opentelemetry-instrument, these lines are redundant
# and cause double instrumentation.
# FastAPIInstrumentor.instrument_app(app)
# Auto-create spans for outbound HTTP (client spans)
# HTTPXClientInstrumentor().instrument()


async def simulate_db_lookup(order_id: str) -> None:
    """Simulate a multi-stage database lookup with nested spans."""
    # 'start_as_current_span' creates a child span and makes it active
    with tracer.start_as_current_span("db.lookup.order") as span:
        # Semantic conventions: standard attributes for DB operations
        span.set_attribute("db.system", "sqlite")
        span.set_attribute("db.statement", "SELECT * FROM orders WHERE id = ?")
        span.set_attribute("telecom.3gpp.slice.service.type", 1)
        span.add_event("db.lookup.start", {"order.id": order_id})
        await asyncio.sleep(0.02)

        with tracer.start_as_current_span("db.lookup.cache") as cache_span:
            cache_span.set_attribute("cache.hit", True)
            cache_span.set_attribute("cache.system", "redis")
            cache_span.add_event("cache.fetch", {"key": f"order:{order_id}"})
            await asyncio.sleep(0.005)

        span.add_event("db.lookup.end", {"row.count": 1})


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
    with tracer.start_as_current_span("o-ran.near-rt-ric.policy") as span:
        span.set_attribute("telecom.o-ran.near_rt_ric.id", "ric-210-ne")
        span.set_attribute("telecom.o-ran.policy.id", "policy-5qi9-lowlat")
        span.add_event(
            "o-ran.policy.apply",
            {"order.id": order_id, "telecom.o-ran.ran.slice": "cot-slice-09"},
        )
        await asyncio.sleep(0.01)


async def check_o_du_health() -> None:
    with tracer.start_as_current_span("o-ran.o-du.health") as span:
        span.set_attribute("telecom.o-ran.o_du.id", "odu-201")
        span.set_attribute("telecom.o-ran.o_du.state", "active")
        span.add_event("o-ran.o-du.heartbeat", {"latency.ms": 2.3})
        await asyncio.sleep(0.008)


async def run_fulfilment_checks(order_id: str) -> None:
    """Run fulfilment checks in parallel (inventory + charging)."""
    await asyncio.gather(
        check_inventory(order_id),
        check_charging_session(order_id),
    )


async def check_inventory(order_id: str) -> None:
    with tracer.start_as_current_span("inventory.check") as span:
        span.set_attribute("app.inventory.region", "eu-central")
        span.add_event("inventory.lookup.start", {"order.id": order_id})

        # Educational Example: Handling Errors
        # If the order_id is 'fail', we simulate an exception and record it.
        if order_id == "fail":
            error_msg = "Inventory system unavailable for this ID"
            # 1. Record the exception event
            span.record_exception(RuntimeError(error_msg))
            # 2. Set the Span Status to Error
            span.set_status(Status(StatusCode.ERROR, error_msg))
            # 3. Raise to propagate up
            raise RuntimeError(error_msg)

        await asyncio.sleep(0.015)
        span.add_event("inventory.lookup.end", {"available": True})


async def check_charging_session(order_id: str) -> None:
    with tracer.start_as_current_span("charging.session.lookup") as span:
        span.set_attribute("telecom.3gpp.pcrf.id", "pcrf-12")
        span.set_attribute("telecom.3gpp.ccf.mode", "online")
        span.add_event("charging.session.start", {"order.id": order_id})
        await asyncio.sleep(0.012)
        span.add_event("charging.session.validated", {"quota.mb": 2048})


@app.get("/orders/{order_id}")
async def get_order(order_id: str, request: Request):
    # Child span inside the request trace (what happened)
    with tracer.start_as_current_span("load-order") as span:
        span.set_attribute("app.order_id", order_id)
        span.set_attribute("telecom.3gpp.session.id", f"pdu-{order_id:0>6}")

        try:
            await asyncio.gather(
                simulate_db_lookup(order_id),
                enrich_with_radio_context(order_id),
                run_fulfilment_checks(order_id),
            )
            span.add_event("order.load.complete")
        except RuntimeError as e:
            # Handle the simulated error from check_inventory
            span.record_exception(e)
            span.set_status(Status(StatusCode.ERROR, str(e)))
            # We can also add a human-readable event
            span.add_event("order.load.failed", {"reason": str(e)})
            # Propagate as HTTP 500
            raise HTTPException(status_code=500, detail=str(e))

    # Outbound call is traced automatically by HTTPX instrumentation
    async with httpx.AsyncClient() as client:
        r = await client.get("https://httpbin.org/delay/1")
        upstream_ms = r.elapsed.total_seconds() * 1000

    # Enrich the active *server* span with request-scoped attributes
    current = trace.get_current_span()
    client_ip = request.client.host if request.client else ""

    current.set_attribute("enduser.id", "u-123")
    current.set_attribute("client.address", client_ip)
    current.set_attribute("app.order_id", order_id)
    current.set_attribute("telecom.3gpp.qos.flow.5qi", 9)
    current.set_attribute("telecom.o-ran.o_du.state", "active")
    current.set_attribute("app.upstream_ms", upstream_ms)

    return {"id": order_id, "status": "ok", "upstream_ms": upstream_ms}
