# Python REST Tracing Demo (FastAPI + OpenTelemetry)

This demo shows how to instrument a **FastAPI** application with **OpenTelemetry**
to generate distributed traces for inbound HTTP requests, custom spans, outbound
HTTP calls, and a telecom-specific semantic-convention extension namespace.

The narrative is a 5G network slice session activation. Standard OTel attributes
describe HTTP, client, network, database, and user context where they already fit.
The custom `telecom.*` attributes show how a telecom domain can extend semantic
conventions for 3GPP, QoS, and O-RAN concepts that are not covered by the base
semantic conventions.

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

The telemetry viewer is included in this repository as a local Go backend. It
receives OTLP gRPC traces, logs, and metrics on `127.0.0.1:4317`, keeps them in
memory, and renders a terminal trace explorer. Start it from the project root:

```bash
go run ./cmd/otel-demo-backend
```

Optional SQLite persistence stores normalized rows and raw OTLP payloads:

```bash
go run ./cmd/otel-demo-backend --db telemetry.sqlite
```

---

## Run the demo app

Start the telemetry backend in one terminal:

```bash
go run ./cmd/otel-demo-backend
```

Then start the FastAPI server in another terminal:

```bash
./start_demo.sh
```

By default the app exports OTLP gRPC to `http://127.0.0.1:4317`. Override it if
you start the backend on a different address:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4317 ./start_demo.sh
```

OpenTelemetry is configured manually inside `app.py` (TracerProvider, OTLP gRPC
exporter, and the FastAPI/HTTPX auto-instrumentors), so the entire setup is
visible in one file. No `opentelemetry-instrument` wrapper is required.

### Demo scenarios

| URL | What it shows |
|---|---|
| `POST http://127.0.0.1:8000/slice-sessions/session-42/activate` | Happy path. The trace moves through subscriber validation, slice/QoS profile resolution, radio context enrichment, RIC admission, charging quota, and a downstream provisioning HTTP call. |
| `POST http://127.0.0.1:8000/slice-sessions/deny/activate` | Error path. The near-RT RIC denies admission, the relevant spans are marked `ERROR`, and the request returns HTTP 403 without noisy duplicate exception events. |

Trigger the scenarios with `curl`:

```bash
curl -X POST http://127.0.0.1:8000/slice-sessions/session-42/activate
curl -X POST http://127.0.0.1:8000/slice-sessions/deny/activate
```

The happy path also calls `POST /provisioning/slice-sessions/{session_id}` via
HTTPX. That creates an outbound client span and a second inbound server span.

The server prints the `trace_id` for each request to the terminal, so you can
match the trace you just triggered against the list in the Go TUI. Use `j`/`k`
or arrow keys to navigate, `enter` to expand or collapse the selected trace,
`tab` to switch panes, and `q` to quit.
