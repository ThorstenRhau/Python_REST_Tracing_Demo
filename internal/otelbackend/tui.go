package otelbackend

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time

type TUI struct {
	store *Store
}

type tuiModel struct {
	store        *Store
	snap         Snapshot
	cursor       int
	selectedPane int
	expanded     map[string]bool
	width        int
	height       int
}

func NewTUI(store *Store) *TUI {
	return &TUI{store: store}
}

func (t *TUI) Run() error {
	_, err := tea.NewProgram(tuiModel{
		store:    t.store,
		snap:     t.store.Snapshot(),
		expanded: make(map[string]bool),
	}).Run()
	return err
}

func (m tuiModel) Init() tea.Cmd {
	return tick()
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		m.snap = m.store.Snapshot()
		if m.cursor >= len(m.snap.Traces) {
			m.cursor = max(0, len(m.snap.Traces)-1)
		}
		return m, tick()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.selectedPane = (m.selectedPane + 1) % 3
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.snap.Traces)-1 {
				m.cursor++
			}
		case "enter":
			if trace := m.selectedTrace(); trace != nil {
				m.expanded[trace.ID] = !m.expanded[trace.ID]
			}
		}
	}
	return m, nil
}

func (m tuiModel) View() string {
	if len(m.snap.Traces) == 0 {
		return "OTel demo backend listening for OTLP gRPC on 127.0.0.1:4317\n\nNo telemetry received yet.\n\nq quit"
	}

	var b strings.Builder
	b.WriteString("OTel demo backend  |  q quit  tab panes  j/k navigate  enter expand\n\n")
	b.WriteString(m.traceListView())
	b.WriteString("\n")
	b.WriteString(m.spanTreeView())
	b.WriteString("\n")
	b.WriteString(m.logsView())
	b.WriteString("\n")
	b.WriteString(m.metricsView())
	return b.String()
}

func (m tuiModel) selectedTrace() *Trace {
	if m.cursor < 0 || m.cursor >= len(m.snap.Traces) {
		return nil
	}
	return m.snap.Traces[m.cursor]
}

func (m tuiModel) traceListView() string {
	var b strings.Builder
	b.WriteString(paneTitle("Traces", m.selectedPane == 0))
	for i, trace := range m.snap.Traces {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		session := trace.SessionID
		if session == "" {
			session = "-"
		}
		b.WriteString(fmt.Sprintf("%s %-24s %-18s spans=%-2d logs=%-2d latest=%s\n",
			cursor, shortID(trace.ID), session, len(trace.Spans), len(trace.Logs), formatClock(trace.LatestActivity)))
	}
	return b.String()
}

func (m tuiModel) spanTreeView() string {
	trace := m.selectedTrace()
	if trace == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(paneTitle("Span Tree", m.selectedPane == 1))
	roots := rootSpans(trace)
	for _, span := range roots {
		m.writeSpan(&b, trace, span, 0)
	}
	return b.String()
}

func (m tuiModel) writeSpan(b *strings.Builder, trace *Trace, span *Span, depth int) {
	marker := " "
	if len(trace.Children[span.SpanID]) > 0 {
		if m.expanded[trace.ID] {
			marker = "-"
		} else {
			marker = "+"
		}
	}
	status := ""
	if span.Status != "" && span.Status != "STATUS_CODE_UNSET" {
		status = " " + span.Status
	}
	b.WriteString(fmt.Sprintf("%s%s %s%s [%s]\n", strings.Repeat("  ", depth), marker, span.Name, status, span.SpanID[:min(8, len(span.SpanID))]))
	if !m.expanded[trace.ID] {
		return
	}
	children := childSpans(trace, span.SpanID)
	for _, child := range children {
		m.writeSpan(b, trace, child, depth+1)
	}
}

func (m tuiModel) logsView() string {
	trace := m.selectedTrace()
	if trace == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(paneTitle("Correlated Logs", m.selectedPane == 2))
	logs := trace.Logs
	if len(logs) > 8 {
		logs = logs[len(logs)-8:]
	}
	for _, log := range logs {
		b.WriteString(fmt.Sprintf("%s %-7s %-8s %s\n", formatClock(log.Time), log.Severity, shortID(log.SpanID), log.Body))
	}
	if len(logs) == 0 {
		b.WriteString("No logs for selected trace.\n")
	}
	return b.String()
}

func (m tuiModel) metricsView() string {
	var b strings.Builder
	b.WriteString(paneTitle("Metrics", false))
	if len(m.snap.Metrics) == 0 {
		b.WriteString("No metrics received yet.\n")
		return b.String()
	}
	for _, metric := range m.snap.Metrics {
		if !demoMetric(metric.Name) {
			continue
		}
		b.WriteString(fmt.Sprintf("%-28s count=%-4d sum=%-8.1f latest=%-8.1f attrs=%s\n",
			metric.Name, metric.Count, metric.Sum, metric.Latest, attrsString(metric.Attributes)))
	}
	return b.String()
}

func paneTitle(title string, active bool) string {
	if active {
		return "[" + title + "]\n"
	}
	return title + "\n"
}

func rootSpans(trace *Trace) []*Span {
	var roots []*Span
	for _, span := range trace.Spans {
		if span.ParentSpanID == "" || trace.Spans[span.ParentSpanID] == nil {
			roots = append(roots, span)
		}
	}
	sortSpans(roots)
	return roots
}

func childSpans(trace *Trace, spanID string) []*Span {
	children := make([]*Span, 0, len(trace.Children[spanID]))
	for _, childID := range trace.Children[spanID] {
		if child := trace.Spans[childID]; child != nil {
			children = append(children, child)
		}
	}
	sortSpans(children)
	return children
}

func sortSpans(spans []*Span) {
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartTime.Before(spans[j].StartTime)
	})
}

func demoMetric(name string) bool {
	return name == "slice_session.requests" ||
		name == "slice_session.duration" ||
		name == "slice_session.active" ||
		name == "ric.admission_decisions"
}

func formatClock(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("15:04:05")
}

func shortID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:16]
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
