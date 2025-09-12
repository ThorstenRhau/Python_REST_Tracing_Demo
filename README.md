# Python REST Tracing Demo (FastAPI + OpenTelemetry)

This demo shows how to instrument a **FastAPI** application with **OpenTelemetry** to generate
distributed traces for inbound HTTP requests, custom spans, and outbound HTTP calls.

---

## 1️⃣ Create and activate a virtual environment

From the project root (where `app.py` lives):

```bash
python3 -m venv .venv
source .venv/bin/activate   # macOS / Linux
# .venv\Scripts\activate    # Windows PowerShell
````

---

## 2️⃣ Install dependencies

With the virtual environment active:

```bash
pip install --upgrade pip

pip install fastapi uvicorn httpx \
    opentelemetry-sdk \
    opentelemetry-instrumentation-fastapi \
    opentelemetry-instrumentation-httpx \
    uvicorn
```

Optional (for sending traces to a backend like Jaeger, Tempo, or Honeycomb):

```bash
pip install opentelemetry-exporter-otlp
```

---

## 3️⃣ Run the demo app

Start the FastAPI server:

```bash
uvicorn app:app --reload
```

Visit [http://127.0.0.1:8000/orders/42](http://127.0.0.1:8000/orders/42)
The terminal will print spans (thanks to the built-in `ConsoleSpanExporter`).

---

## 4️⃣ Explore traces

You’ll see a parent/child span structure similar to:

```
HTTP GET /orders/42      ← server span (auto)
 ├─ load-order           ← custom span
 └─ HTTP GET httpbin.org ← client span (auto)
```

If you installed an OTLP exporter and have a collector (Jaeger/Tempo/Honeycomb etc.)
running at `http://localhost:4318`, replace the `ConsoleSpanExporter`
in `app.py` with an `OTLPSpanExporter` to ship traces to that backend.

---

## 5️⃣ Clean up

Deactivate the environment when done:

```bash
deactivate
```

---

### Files

```
.
├─ app.py        # The FastAPI demo application with tracing
└─ README.md     # This guide
```

Enjoy exploring your very own distributed traces!
