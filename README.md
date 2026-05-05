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

Visit [http://127.0.0.1:8000/orders/42](http://127.0.0.1:8000/orders/42)

You should now see traces being displayed in the otel-tui application. Press
ENTER to see the spans for each trace.

---

Enjoy exploring your very own cloud native traces!
