# app.py
from fastapi import FastAPI, Request
import httpx
import asyncio

# --- OpenTelemetry setup (essentials) ---
from opentelemetry import trace
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor, ConsoleSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor

# Configure a tracer provider with a service name
provider = TracerProvider(resource=Resource.create({"service.name": "orders-api"}))
provider.add_span_processor(
    BatchSpanProcessor(ConsoleSpanExporter())
)  # demo: print spans
trace.set_tracer_provider(provider)
tracer = trace.get_tracer(__name__)

app = FastAPI()

# Auto-create spans for inbound requests
FastAPIInstrumentor.instrument_app(app)
# Auto-create spans for outbound HTTP
HTTPXClientInstrumentor().instrument()


@app.get("/orders/{order_id}")
async def get_order(order_id: str, request: Request):
    # Custom work inside the request trace
    with tracer.start_as_current_span("load-order") as span:
        span.set_attribute("app.order_id", order_id)
        # Simulate DB call (child of the request span) without blocking the loop
        await asyncio.sleep(0.02)
        span.add_event("db.query.start", {"sql": "SELECT * FROM orders WHERE id=?"})

    # Outbound call is traced by the HTTPX instrumentation
    async with httpx.AsyncClient() as client:
        r = await client.get("https://httpbin.org/delay/1")  # no empty f-string
        upstream_ms = r.elapsed.total_seconds() * 1000

    # Enrich the active server span with domain attributes
    current = trace.get_current_span()
    client_ip = request.client.host if request.client else "unknown"
    current.set_attribute("enduser.id", "u-123")
    current.set_attribute("net.peer.ip", client_ip)
    current.set_attribute("app.upstream_ms", upstream_ms)

    return {"id": order_id, "status": "ok", "upstream_ms": upstream_ms}
