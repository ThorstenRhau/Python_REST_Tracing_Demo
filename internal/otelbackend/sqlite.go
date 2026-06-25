package otelbackend

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"
)

type Persistence struct {
	db *sql.DB
}

func OpenPersistence(ctx context.Context, path string) (*Persistence, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	p := &Persistence{db: db}
	if err := p.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return p, nil
}

func (p *Persistence) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}

func (p *Persistence) WriteRaw(ctx context.Context, signal string, payload []byte) error {
	if p == nil {
		return nil
	}
	_, err := p.db.ExecContext(ctx, `insert into raw_otlp(signal, received_at, payload) values (?, ?, ?)`, signal, time.Now().UTC().Format(time.RFC3339Nano), payload)
	return err
}

func (p *Persistence) WriteSnapshot(ctx context.Context, snap Snapshot) error {
	if p == nil {
		return nil
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, trace := range snap.Traces {
		if _, err := tx.ExecContext(ctx, `
insert into traces(trace_id, name, session_id, resource_json, latest_activity)
values (?, ?, ?, ?, ?)
on conflict(trace_id) do update set
  name=excluded.name,
  session_id=excluded.session_id,
  resource_json=excluded.resource_json,
  latest_activity=excluded.latest_activity`,
			trace.ID, trace.Name, trace.SessionID, jsonText(trace.Resource), timeText(trace.LatestActivity)); err != nil {
			return err
		}

		for _, span := range trace.Spans {
			if _, err := tx.ExecContext(ctx, `
insert into spans(trace_id, span_id, parent_span_id, name, kind, status, session_id, start_time, end_time, attributes_json, resource_json, events_json)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(trace_id, span_id) do update set
  parent_span_id=excluded.parent_span_id,
  name=excluded.name,
  kind=excluded.kind,
  status=excluded.status,
  session_id=excluded.session_id,
  start_time=excluded.start_time,
  end_time=excluded.end_time,
  attributes_json=excluded.attributes_json,
  resource_json=excluded.resource_json,
  events_json=excluded.events_json`,
				span.TraceID, span.SpanID, span.ParentSpanID, span.Name, span.Kind, span.Status, span.SessionID, timeText(span.StartTime), timeText(span.EndTime), jsonText(span.Attributes), jsonText(span.Resource), jsonText(span.Events)); err != nil {
				return err
			}
		}

		for _, log := range trace.Logs {
			if _, err := tx.ExecContext(ctx, `
insert or ignore into logs(trace_id, span_id, session_id, time, severity, body, attributes_json, resource_json)
values (?, ?, ?, ?, ?, ?, ?, ?)`,
				log.TraceID, log.SpanID, log.SessionID, timeText(log.Time), log.Severity, log.Body, jsonText(log.Attributes), jsonText(log.Resource)); err != nil {
				return err
			}
		}
	}

	for _, metric := range snap.Metrics {
		if _, err := tx.ExecContext(ctx, `
insert into metrics(name, resource_json, attributes_json, count, sum, latest, updated_at)
values (?, ?, ?, ?, ?, ?, ?)`,
			metric.Name, jsonText(metric.Resource), jsonText(metric.Attributes), metric.Count, metric.Sum, metric.Latest, timeText(metric.UpdatedAt)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (p *Persistence) migrate(ctx context.Context) error {
	stmts := []string{
		`create table if not exists raw_otlp (
			id integer primary key autoincrement,
			signal text not null,
			received_at text not null,
			payload blob not null
		)`,
		`create table if not exists traces (
			trace_id text primary key,
			name text,
			session_id text,
			resource_json text not null,
			latest_activity text
		)`,
		`create table if not exists spans (
			trace_id text not null,
			span_id text not null,
			parent_span_id text,
			name text,
			kind text,
			status text,
			session_id text,
			start_time text,
			end_time text,
			attributes_json text not null,
			resource_json text not null,
			events_json text not null,
			primary key(trace_id, span_id)
		)`,
		`create table if not exists logs (
			id integer primary key autoincrement,
			trace_id text,
			span_id text,
			session_id text,
			time text,
			severity text,
			body text,
			attributes_json text not null,
			resource_json text not null
		)`,
		`create unique index if not exists logs_dedupe on logs(trace_id, span_id, session_id, time, severity, body)`,
		`create table if not exists metrics (
			id integer primary key autoincrement,
			name text not null,
			resource_json text not null,
			attributes_json text not null,
			count integer not null,
			sum real not null,
			latest real not null,
			updated_at text
		)`,
	}
	for _, stmt := range stmts {
		if _, err := p.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
