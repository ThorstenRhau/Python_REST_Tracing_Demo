# Python REST Tracing Demo (FastAPI + OpenTelemetry)

This demo shows how to instrument a **FastAPI** application with **OpenTelemetry** to generate
distributed traces for inbound HTTP requests, custom spans, and outbound HTTP calls.

---

## Install uv

If you don't have `uv` installed yet:

```bash
# macOS / Linux
curl -LsSf https://astral.sh/uv/install.sh | sh

# Or with Homebrew on macOS
brew install uv
```

See https://docs.astral.sh/uv/ for other installation options.

---

## Create a virtual environment

From the project root (where `app.py` lives):

```bash
uv venv
source .venv/bin/activate   # macOS / Linux
# .venv\Scripts\activate    # Windows PowerShell
```

---

## Install dependencies

With the virtual environment active, install everything listed in
`requirements.txt`:

```bash
uv pip install -r requirements.txt
```

Install otel-tui application with home-brew on macOS

```bash
brew install ymtdzzz/tap/otel-tui
```

If you are not on macOS please visit https://github.com/ymtdzzz/otel-tui to find
out how to install it on your operating system

---

## Run the demo app

Start the FastAPI server:

```bash
./start_demo.sh
```

Start the OTel TUI in a separate terminal

```bash
otel-tui
```

OpenTelemetry is configured manually inside `app.py` (TracerProvider, OTLP gRPC
exporter, and the FastAPI/HTTPX auto-instrumentors), so the entire setup is
visible in one file — no `opentelemetry-instrument` wrapper required.

### Demo scenarios

| URL | What it shows |
|---|---|
| [/orders/42](http://127.0.0.1:8000/orders/42) | Happy path. Three sequential stages under `order.load` — `db.lookup.order`, `radio.context.enrichment`, `fulfilment.checks` — each with parallel children, followed by a ~1 s outbound HTTPX call. |
| [/orders/fail](http://127.0.0.1:8000/orders/fail) | Error path. `inventory.check` records an exception and sets the span status to ERROR; `order.load` propagates the failure and the request returns HTTP 500. |

The server prints the `trace_id` for each request to the terminal, so you can
match the trace you just triggered against the list in otel-tui. Press ENTER on
a trace to see the full span tree.

---

Enjoy exploring your very own cloud native traces!
