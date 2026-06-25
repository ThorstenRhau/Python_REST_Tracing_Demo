package otelbackend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

var (
	testTraceID = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	rootSpanID  = []byte{1, 1, 1, 1, 1, 1, 1, 1}
	childSpanID = []byte{2, 2, 2, 2, 2, 2, 2, 2}
)

func TestIngestTraceBuildsParentChildTree(t *testing.T) {
	store := NewStore()
	store.IngestTraces(sampleResourceSpans())

	snap := store.Snapshot()
	if got := len(snap.Traces); got != 1 {
		t.Fatalf("traces = %d, want 1", got)
	}
	trace := snap.Traces[0]
	if trace.ID != bytesID(testTraceID) {
		t.Fatalf("trace id = %q", trace.ID)
	}
	if trace.SessionID != "session-42" {
		t.Fatalf("session id = %q", trace.SessionID)
	}
	children := trace.Children[bytesID(rootSpanID)]
	if len(children) != 1 || children[0] != bytesID(childSpanID) {
		t.Fatalf("children = %#v", children)
	}
}

func TestIngestLogsCorrelatesTraceSpanAndSession(t *testing.T) {
	store := NewStore()
	store.IngestTraces(sampleResourceSpans())
	store.IngestLogs(sampleResourceLogs())

	trace := store.Snapshot().Traces[0]
	if got := len(trace.Logs); got != 1 {
		t.Fatalf("logs = %d, want 1", got)
	}
	log := trace.Logs[0]
	if log.TraceID != bytesID(testTraceID) || log.SpanID != bytesID(childSpanID) || log.SessionID != "session-42" {
		t.Fatalf("unexpected log correlation: %#v", log)
	}
}

func TestIngestMetricsSummarizesDemoSignals(t *testing.T) {
	store := NewStore()
	store.IngestMetrics(sampleResourceMetrics())
	store.IngestMetrics(sampleResourceMetrics())

	metrics := store.Snapshot().Metrics
	byName := map[string]*MetricSummary{}
	for _, metric := range metrics {
		byName[metric.Name] = metric
	}
	if byName["slice_session.requests"].Latest != 2 {
		t.Fatalf("requests latest = %v", byName["slice_session.requests"].Latest)
	}
	if byName["slice_session.duration"].Count != 3 || byName["slice_session.duration"].Sum != 153.5 {
		t.Fatalf("duration summary = %#v", byName["slice_session.duration"])
	}
	if byName["ric.admission_decisions"].Attributes["decision"] != "accepted" {
		t.Fatalf("ric attributes = %#v", byName["ric.admission_decisions"].Attributes)
	}
}

func TestSQLitePersistenceWritesTelemetry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telemetry.sqlite")
	p, err := OpenPersistence(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	store := NewStore()
	store.IngestTraces(sampleResourceSpans())
	store.IngestLogs(sampleResourceLogs())
	store.IngestMetrics(sampleResourceMetrics())

	if err := p.WriteRaw(ctx, "traces", []byte("raw")); err != nil {
		t.Fatal(err)
	}
	if err := p.WriteSnapshot(ctx, store.Snapshot()); err != nil {
		t.Fatal(err)
	}

	for table, want := range map[string]int{"raw_otlp": 1, "traces": 1, "spans": 2, "logs": 1, "metrics": 3} {
		var got int
		if err := p.db.QueryRowContext(ctx, "select count(*) from "+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s rows = %d, want %d", table, got, want)
		}
	}
}

func TestMemoryOnlyDoesNotCreateSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-only.sqlite")
	store := NewStore()
	store.IngestTraces(sampleResourceSpans())

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("sqlite file exists or stat failed differently: %v", err)
	}
}

func sampleResourceSpans() []*tracev1.ResourceSpans {
	start := uint64(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC).UnixNano())
	return []*tracev1.ResourceSpans{{
		Resource: sampleResource(),
		ScopeSpans: []*tracev1.ScopeSpans{{
			Spans: []*tracev1.Span{
				{
					TraceId:           testTraceID,
					SpanId:            rootSpanID,
					Name:              "slice_session.activate",
					StartTimeUnixNano: start,
					EndTimeUnixNano:   start + 100,
					Attributes:        attrs("app.session.id", "session-42"),
				},
				{
					TraceId:           testTraceID,
					SpanId:            childSpanID,
					ParentSpanId:      rootSpanID,
					Name:              "ric.admission_decision",
					StartTimeUnixNano: start + 10,
					EndTimeUnixNano:   start + 20,
					Attributes:        attrs("app.session.id", "session-42"),
				},
			},
		}},
	}}
}

func sampleResourceLogs() []*logsv1.ResourceLogs {
	return []*logsv1.ResourceLogs{{
		Resource: sampleResource(),
		ScopeLogs: []*logsv1.ScopeLogs{{
			LogRecords: []*logsv1.LogRecord{{
				TraceId:              testTraceID,
				SpanId:               childSpanID,
				TimeUnixNano:         uint64(time.Date(2026, 6, 25, 12, 0, 1, 0, time.UTC).UnixNano()),
				SeverityText:         "INFO",
				Body:                 stringValue("RIC admission accepted"),
				Attributes:           attrs("app.session.id", "session-42"),
				ObservedTimeUnixNano: 0,
			}},
		}},
	}}
}

func sampleResourceMetrics() []*metricsv1.ResourceMetrics {
	return []*metricsv1.ResourceMetrics{{
		Resource: sampleResource(),
		ScopeMetrics: []*metricsv1.ScopeMetrics{{
			Metrics: []*metricsv1.Metric{
				{
					Name: "slice_session.requests",
					Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{DataPoints: []*metricsv1.NumberDataPoint{intPoint(2, attrs("session_id", "session-42"))}}},
				},
				{
					Name: "slice_session.duration",
					Data: &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{DataPoints: []*metricsv1.HistogramDataPoint{{
						Attributes: attrs("status", "success"),
						Count:      3,
						Sum:        floatPtr(153.5),
					}}}},
				},
				{
					Name: "ric.admission_decisions",
					Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{DataPoints: []*metricsv1.NumberDataPoint{intPoint(1, attrs("decision", "accepted"))}}},
				},
			},
		}},
	}}
}

func sampleResource() *resourcev1.Resource {
	return &resourcev1.Resource{Attributes: attrs("service.name", "slice-activation-api")}
}

func attrs(key, value string) []*commonv1.KeyValue {
	return []*commonv1.KeyValue{{Key: key, Value: stringValue(value)}}
}

func stringValue(value string) *commonv1.AnyValue {
	return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}
}

func intPoint(value int64, attrs []*commonv1.KeyValue) *metricsv1.NumberDataPoint {
	return &metricsv1.NumberDataPoint{
		Attributes: attrs,
		Value:      &metricsv1.NumberDataPoint_AsInt{AsInt: value},
	}
}

func floatPtr(value float64) *float64 {
	return &value
}
