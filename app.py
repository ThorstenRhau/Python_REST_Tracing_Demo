# app.py
from fastapi import FastAPI, Request
import httpx
import asyncio

# --- OpenTelemetry setup (essentials) ---
from opentelemetry import trace
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
# If you want HTTP instead of gRPC, swap the import:
# from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor


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
}

# --- Configure provider and exporter ---
provider = TracerProvider(resource=Resource.create(TELECOM_RESOURCE))

# Export spans to OTLP gRPC on localhost (otel-tui default is 4317)
# For demo purposes clear text telemetry is used, in production
# OTel and OTel components suppoort mTLS
otlp_exporter = OTLPSpanExporter(endpoint="http://localhost:4317", insecure=True)
provider.add_span_processor(BatchSpanProcessor(otlp_exporter))

trace.set_tracer_provider(provider)
tracer = trace.get_tracer(__name__)

# --- FastAPI app + auto instrumentation ---
app = FastAPI()

# Auto-create spans for inbound requests (server spans)
FastAPIInstrumentor.instrument_app(app)
# Auto-create spans for outbound HTTP (client spans)
HTTPXClientInstrumentor().instrument()


@app.get("/orders/{order_id}")
async def get_order(order_id: str, request: Request):
    # Child span inside the request trace (what happened)
    with tracer.start_as_current_span("load-order") as span:
        span.set_attribute("app.order_id", order_id)
        await asyncio.sleep(0.02)  # simulate DB latency
        span.add_event("db.query.start", {"sql": "SELECT * FROM orders WHERE id=?"})

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
    current.set_attribute("app.upstream_ms", upstream_ms)

    return {"id": order_id, "status": "ok", "upstream_ms": upstream_ms}
