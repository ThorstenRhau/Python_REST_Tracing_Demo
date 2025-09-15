#!/usr/bin/env sh
set -eu

# Resolve script directory and venv location
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VENV_DIR="$SCRIPT_DIR/.venv"

OPENTELE="$VENV_DIR/bin/opentelemetry-instrument"
UVICORN="$VENV_DIR/bin/uvicorn"

# Ensure venv exists
if [ ! -d "$VENV_DIR" ]; then
    printf ".venv directory not found.\n" >&2
    printf "Please set up your python venv with dependencies described in README.md.\n" >&2
    exit 1
fi

# Ensure required executables exist
if [ ! -x "$OPENTELE" ]; then
    printf "opentelemetry-instrument not found in venv: %s\n" "$OPENTELE" >&2
    exit 1
fi

if [ ! -x "$UVICORN" ]; then
    printf "uvicorn not found in venv: %s\n" "$UVICORN" >&2
    exit 1
fi

printf "################################\n"
printf "Start otel-tui and connect to\n"
printf "http://127.0.0.1:8000/orders/999\n"
printf "################################\n"

# Launch with OpenTelemetry instrumentation, replacing the shell
exec "$OPENTELE" "$UVICORN" app:app --reload

# Optional: automatically open the URL on macOS (uncomment to enable)
# if [ "$(uname)" = "Darwin" ]; then
#   (sleep 0.8; open "http://127.0.0.1:8000/orders/999") &
# fi
