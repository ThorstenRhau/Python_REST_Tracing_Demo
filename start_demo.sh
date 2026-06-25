#!/usr/bin/env sh
set -eu

# Resolve script directory and venv location
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VENV_DIR="$SCRIPT_DIR/.venv"

UVICORN="$VENV_DIR/bin/uvicorn"

# Ensure venv exists
if [ ! -d "$VENV_DIR" ]; then
    printf ".venv directory not found.\n" >&2
    printf "Please set up your python venv with dependencies described in README.md.\n" >&2
    exit 1
fi

if [ ! -x "$UVICORN" ]; then
    printf "uvicorn not found in venv: %s\n" "$UVICORN" >&2
    exit 1
fi

printf "############################################################\n"
printf "Start the Go telemetry backend in another terminal:\n"
printf "  go run ./cmd/otel-demo-backend\n"
printf "\n"
printf "Then trigger demo requests:\n"
printf "  curl -X POST http://127.0.0.1:8000/slice-sessions/session-42/activate\n"
printf "  curl -X POST http://127.0.0.1:8000/slice-sessions/deny/activate\n"
printf "############################################################\n"

# OTel is configured manually in app.py; no opentelemetry-instrument wrapper.
exec "$UVICORN" app:app --reload
