#!/usr/bin/env sh

./.venv/bin/opentelemetry-instrument ./.venv/bin/uvicorn app:app --reload
