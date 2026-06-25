package otelbackend

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

type Store struct {
	mu      sync.RWMutex
	traces  map[string]*Trace
	metrics map[string]*MetricSummary
}

type Trace struct {
	ID             string
	Name           string
	SessionID      string
	Resource       map[string]string
	Spans          map[string]*Span
	Children       map[string][]string
	Logs           []*LogRecord
	LatestActivity time.Time
}

type Span struct {
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Name          string
	Kind          string
	Status        string
	SessionID     string
	StartTime     time.Time
	EndTime       time.Time
	Attributes    map[string]string
	Resource      map[string]string
	Events        []SpanEvent
	LatestLogTime time.Time
}

type SpanEvent struct {
	Name       string
	Time       time.Time
	Attributes map[string]string
}

type LogRecord struct {
	TraceID    string
	SpanID     string
	SessionID  string
	Time       time.Time
	Severity   string
	Body       string
	Attributes map[string]string
	Resource   map[string]string
}

type MetricSummary struct {
	Name       string
	Resource   map[string]string
	Attributes map[string]string
	Count      uint64
	Sum        float64
	Latest     float64
	UpdatedAt  time.Time
}

type Snapshot struct {
	Traces  []*Trace
	Metrics []*MetricSummary
}

func NewStore() *Store {
	return &Store{
		traces:  make(map[string]*Trace),
		metrics: make(map[string]*MetricSummary),
	}
}

func (s *Store) IngestTraces(resourceSpans []*tracev1.ResourceSpans) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, resourceSpans := range resourceSpans {
		resource := attrsFromResource(resourceSpans.GetResource())
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, protoSpan := range scopeSpans.GetSpans() {
				traceID := bytesID(protoSpan.GetTraceId())
				spanID := bytesID(protoSpan.GetSpanId())
				if traceID == "" || spanID == "" {
					continue
				}

				trace := s.ensureTrace(traceID)
				trace.Resource = mergeKeep(trace.Resource, resource)

				span := &Span{
					TraceID:      traceID,
					SpanID:       spanID,
					ParentSpanID: bytesID(protoSpan.GetParentSpanId()),
					Name:         protoSpan.GetName(),
					Kind:         protoSpan.GetKind().String(),
					Status:       protoSpan.GetStatus().GetCode().String(),
					StartTime:    unixNano(protoSpan.GetStartTimeUnixNano()),
					EndTime:      unixNano(protoSpan.GetEndTimeUnixNano()),
					Attributes:   attrsFromKVs(protoSpan.GetAttributes()),
					Resource:     resource,
				}
				for _, event := range protoSpan.GetEvents() {
					span.Events = append(span.Events, SpanEvent{
						Name:       event.GetName(),
						Time:       unixNano(event.GetTimeUnixNano()),
						Attributes: attrsFromKVs(event.GetAttributes()),
					})
				}
				span.SessionID = firstNonEmpty(span.Attributes["app.session.id"], span.Attributes["session_id"])
				trace.Spans[spanID] = span
				if span.ParentSpanID != "" {
					trace.Children[span.ParentSpanID] = appendUnique(trace.Children[span.ParentSpanID], spanID)
				}
				if trace.Name == "" || span.ParentSpanID == "" {
					trace.Name = span.Name
				}
				if trace.SessionID == "" {
					trace.SessionID = span.SessionID
				}
				trace.LatestActivity = maxTime(trace.LatestActivity, maxTime(span.EndTime, span.StartTime))
			}
		}
	}
}

func (s *Store) IngestLogs(resourceLogs []*logsv1.ResourceLogs) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, resourceLogs := range resourceLogs {
		resource := attrsFromResource(resourceLogs.GetResource())
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			for _, protoLog := range scopeLogs.GetLogRecords() {
				traceID := bytesID(protoLog.GetTraceId())
				spanID := bytesID(protoLog.GetSpanId())
				attrs := attrsFromKVs(protoLog.GetAttributes())
				sessionID := firstNonEmpty(attrs["app.session.id"], attrs["session_id"], attrs["session.id"])
				rec := &LogRecord{
					TraceID:    traceID,
					SpanID:     spanID,
					SessionID:  sessionID,
					Time:       firstTime(unixNano(protoLog.GetTimeUnixNano()), unixNano(protoLog.GetObservedTimeUnixNano())),
					Severity:   firstNonEmpty(protoLog.GetSeverityText(), protoLog.GetSeverityNumber().String()),
					Body:       anyValueString(protoLog.GetBody()),
					Attributes: attrs,
					Resource:   resource,
				}

				if traceID != "" {
					trace := s.ensureTrace(traceID)
					trace.Resource = mergeKeep(trace.Resource, resource)
					trace.Logs = append(trace.Logs, rec)
					trace.LatestActivity = maxTime(trace.LatestActivity, rec.Time)
					if trace.SessionID == "" {
						trace.SessionID = sessionID
					}
					if span := trace.Spans[spanID]; span != nil {
						span.LatestLogTime = maxTime(span.LatestLogTime, rec.Time)
					}
				}
			}
		}
	}
}

func (s *Store) IngestMetrics(resourceMetrics []*metricsv1.ResourceMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, resourceMetrics := range resourceMetrics {
		resource := attrsFromResource(resourceMetrics.GetResource())
		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			for _, metric := range scopeMetrics.GetMetrics() {
				for _, point := range metricPoints(metric) {
					attrs := attrsFromKVs(point.attrs)
					key := metricKey(metric.GetName(), resource, attrs)
					summary := s.metrics[key]
					if summary == nil {
						summary = &MetricSummary{Name: metric.GetName(), Resource: resource, Attributes: attrs}
						s.metrics[key] = summary
					}
					summary.Count = point.count
					summary.Sum = point.sum
					summary.Latest = point.latest
					summary.UpdatedAt = now
				}
			}
		}
	}
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	traces := make([]*Trace, 0, len(s.traces))
	for _, trace := range s.traces {
		traces = append(traces, cloneTrace(trace))
	}
	sort.Slice(traces, func(i, j int) bool {
		return traces[i].LatestActivity.After(traces[j].LatestActivity)
	})

	metrics := make([]*MetricSummary, 0, len(s.metrics))
	for _, metric := range s.metrics {
		metrics = append(metrics, cloneMetric(metric))
	}
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].Name == metrics[j].Name {
			return attrsString(metrics[i].Attributes) < attrsString(metrics[j].Attributes)
		}
		return metrics[i].Name < metrics[j].Name
	})

	return Snapshot{Traces: traces, Metrics: metrics}
}

func (s *Store) ensureTrace(traceID string) *Trace {
	trace := s.traces[traceID]
	if trace == nil {
		trace = &Trace{ID: traceID, Spans: make(map[string]*Span), Children: make(map[string][]string)}
		s.traces[traceID] = trace
	}
	return trace
}

type metricPoint struct {
	attrs  []*commonv1.KeyValue
	count  uint64
	sum    float64
	latest float64
}

func metricPoints(metric *metricsv1.Metric) []metricPoint {
	var points []metricPoint
	switch data := metric.GetData().(type) {
	case *metricsv1.Metric_Sum:
		for _, point := range data.Sum.GetDataPoints() {
			points = append(points, metricPoint{attrs: point.GetAttributes(), count: 1, sum: numberPointValue(point), latest: numberPointValue(point)})
		}
	case *metricsv1.Metric_Gauge:
		for _, point := range data.Gauge.GetDataPoints() {
			points = append(points, metricPoint{attrs: point.GetAttributes(), count: 1, sum: numberPointValue(point), latest: numberPointValue(point)})
		}
	case *metricsv1.Metric_Histogram:
		for _, point := range data.Histogram.GetDataPoints() {
			points = append(points, metricPoint{attrs: point.GetAttributes(), count: point.GetCount(), sum: point.GetSum(), latest: point.GetSum()})
		}
	}
	return points
}

func numberPointValue(point *metricsv1.NumberDataPoint) float64 {
	switch v := point.GetValue().(type) {
	case *metricsv1.NumberDataPoint_AsDouble:
		return v.AsDouble
	case *metricsv1.NumberDataPoint_AsInt:
		return float64(v.AsInt)
	default:
		return 0
	}
}

func attrsFromResource(resource *resourcev1.Resource) map[string]string {
	if resource == nil {
		return map[string]string{}
	}
	return attrsFromKVs(resource.GetAttributes())
}

func attrsFromKVs(kvs []*commonv1.KeyValue) map[string]string {
	attrs := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		attrs[kv.GetKey()] = anyValueString(kv.GetValue())
	}
	return attrs
}

func anyValueString(v *commonv1.AnyValue) string {
	switch value := v.GetValue().(type) {
	case *commonv1.AnyValue_StringValue:
		return value.StringValue
	case *commonv1.AnyValue_BoolValue:
		return fmt.Sprintf("%t", value.BoolValue)
	case *commonv1.AnyValue_IntValue:
		return fmt.Sprintf("%d", value.IntValue)
	case *commonv1.AnyValue_DoubleValue:
		return fmt.Sprintf("%g", value.DoubleValue)
	case *commonv1.AnyValue_BytesValue:
		return hex.EncodeToString(value.BytesValue)
	case *commonv1.AnyValue_ArrayValue:
		parts := make([]string, 0, len(value.ArrayValue.GetValues()))
		for _, item := range value.ArrayValue.GetValues() {
			parts = append(parts, anyValueString(item))
		}
		return strings.Join(parts, ",")
	case *commonv1.AnyValue_KvlistValue:
		return attrsString(attrsFromKVs(value.KvlistValue.GetValues()))
	default:
		return ""
	}
}

func bytesID(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return hex.EncodeToString(b)
}

func unixNano(ns uint64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(ns))
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func mergeKeep(base, next map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	for key, value := range next {
		if _, ok := base[key]; !ok {
			base[key] = value
		}
	}
	return base
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func metricKey(name string, resource, attrs map[string]string) string {
	return name + "|" + attrsString(resource) + "|" + attrsString(attrs)
}

func attrsString(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+attrs[key])
	}
	return strings.Join(parts, ",")
}

func cloneTrace(trace *Trace) *Trace {
	clone := &Trace{
		ID:             trace.ID,
		Name:           trace.Name,
		SessionID:      trace.SessionID,
		Resource:       cloneMap(trace.Resource),
		Spans:          make(map[string]*Span, len(trace.Spans)),
		Children:       make(map[string][]string, len(trace.Children)),
		Logs:           make([]*LogRecord, 0, len(trace.Logs)),
		LatestActivity: trace.LatestActivity,
	}
	for id, span := range trace.Spans {
		clone.Spans[id] = cloneSpan(span)
	}
	for id, children := range trace.Children {
		clone.Children[id] = append([]string(nil), children...)
	}
	for _, log := range trace.Logs {
		clone.Logs = append(clone.Logs, cloneLog(log))
	}
	sort.Slice(clone.Logs, func(i, j int) bool {
		return clone.Logs[i].Time.Before(clone.Logs[j].Time)
	})
	return clone
}

func cloneSpan(span *Span) *Span {
	clone := *span
	clone.Attributes = cloneMap(span.Attributes)
	clone.Resource = cloneMap(span.Resource)
	clone.Events = append([]SpanEvent(nil), span.Events...)
	return &clone
}

func cloneLog(log *LogRecord) *LogRecord {
	clone := *log
	clone.Attributes = cloneMap(log.Attributes)
	clone.Resource = cloneMap(log.Resource)
	return &clone
}

func cloneMetric(metric *MetricSummary) *MetricSummary {
	clone := *metric
	clone.Resource = cloneMap(metric.Resource)
	clone.Attributes = cloneMap(metric.Attributes)
	return &clone
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
