package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Service struct {
	startTime      time.Time
	clickCounter   int64
	pageViews      int64
	sessionCounter int64

	// Metrics
	clickRate       metric.Int64Counter
	cursorPositions metric.Int64Histogram
	requestDuration metric.Float64Histogram
	activeUsers     metric.Int64UpDownCounter
	httpRequests    metric.Int64Counter

	// Thread-safe collections
	sessions     map[string]*SessionData
	sessionMutex sync.RWMutex

	// OpenTelemetry
	tracer trace.Tracer
	meter  metric.Meter
}

type SessionData struct {
	ID         string
	StartTime  time.Time
	LastActive time.Time
	ClickCount int64
	Events     []TrackingEvent
}

type TrackingEvent struct {
	EventType   string                 `json:"event_type"`
	Timestamp   time.Time              `json:"timestamp"`
	CursorX     int                    `json:"cursor_x"`
	CursorY     int                    `json:"cursor_y"`
	ElementID   string                 `json:"element_id"`
	ElementType string                 `json:"element_type"`
	PageURL     string                 `json:"page_url"`
	UserAgent   string                 `json:"user_agent"`
	SessionID   string                 `json:"session_id"`
	ViewportX   int                    `json:"viewport_x"`
	ViewportY   int                    `json:"viewport_y"`
	ScrollX     int                    `json:"scroll_x"`
	ScrollY     int                    `json:"scroll_y"`
	ElementText string                 `json:"element_text"`
	Custom      map[string]interface{} `json:"custom,omitempty"`
}

type HealthMetrics struct {
	Uptime        string `json:"uptime"`
	TotalClicks   int64  `json:"total_clicks"`
	PageViews     int64  `json:"page_views"`
	ActiveUsers   int64  `json:"active_users"`
	TotalSessions int64  `json:"total_sessions"`
}

func New() *Service {
	tracer := otel.Tracer("worker-service")
	meter := otel.Meter("worker-service")

	// Initialize metrics
	clickRate, _ := meter.Int64Counter("worker_clicks_total",
		metric.WithDescription("Total number of clicks recorded"))

	cursorPositions, _ := meter.Int64Histogram("worker_cursor_positions",
		metric.WithDescription("Cursor position coordinates"))

	requestDuration, _ := meter.Float64Histogram("worker_http_request_duration_seconds",
		metric.WithDescription("HTTP request duration in seconds"))

	activeUsers, _ := meter.Int64UpDownCounter("worker_active_users",
		metric.WithDescription("Number of active users"))

	httpRequests, _ := meter.Int64Counter("worker_http_requests_total",
		metric.WithDescription("Total HTTP requests processed"))

	return &Service{
		startTime:       time.Now(),
		sessions:        make(map[string]*SessionData),
		tracer:          tracer,
		meter:           meter,
		clickRate:       clickRate,
		cursorPositions: cursorPositions,
		requestDuration: requestDuration,
		activeUsers:     activeUsers,
		httpRequests:    httpRequests,
	}
}

func (s *Service) ProcessTrackingEvent(ctx context.Context, event TrackingEvent) error {
	ctx, span := s.tracer.Start(ctx, "process_tracking_event")
	defer span.End()

	// Add span attributes
	span.SetAttributes(
		attribute.String("event.type", event.EventType),
		attribute.String("session.id", event.SessionID),
		attribute.Int("cursor.x", event.CursorX),
		attribute.Int("cursor.y", event.CursorY),
	)

	// Update session data
	s.updateSession(event)

	// Record different metrics based on event type
	switch event.EventType {
	case "click":
		s.recordClick(ctx, event)
	case "mousemove":
		s.recordCursorPosition(ctx, event)
	case "scroll":
		s.recordScrollEvent(ctx, event)
	case "custom":
		s.recordCustomEvent(ctx, event)
	default:
		slog.Warn("Unknown event type", "type", event.EventType)
	}

	// Log event for structured logging
	slog.Info("Event processed",
		"event_type", event.EventType,
		"session_id", event.SessionID,
		"element_id", event.ElementID,
		"timestamp", event.Timestamp,
	)

	return nil
}

func (s *Service) recordClick(ctx context.Context, event TrackingEvent) {
	// Increment global click counter
	atomic.AddInt64(&s.clickCounter, 1)

	// Record click rate metric
	s.clickRate.Add(ctx, 1, metric.WithAttributes(
		attribute.String("element_id", event.ElementID),
		attribute.String("element_type", event.ElementType),
		attribute.String("page_url", event.PageURL),
	))

	// Record cursor position at click
	s.cursorPositions.Record(ctx, int64(event.CursorX), metric.WithAttributes(
		attribute.String("coordinate", "x"),
		attribute.String("event_type", "click"),
	))
	s.cursorPositions.Record(ctx, int64(event.CursorY), metric.WithAttributes(
		attribute.String("coordinate", "y"),
		attribute.String("event_type", "click"),
	))

	// Create custom span for click analytics
	_, clickSpan := s.tracer.Start(ctx, "click_analytics")
	clickSpan.SetAttributes(
		attribute.String("click.element_id", event.ElementID),
		attribute.String("click.element_type", event.ElementType),
		attribute.String("click.element_text", event.ElementText),
		attribute.Int("click.position_x", event.CursorX),
		attribute.Int("click.position_y", event.CursorY),
		attribute.Int("click.viewport_x", event.ViewportX),
		attribute.Int("click.viewport_y", event.ViewportY),
	)
	clickSpan.End()
}

func (s *Service) recordCursorPosition(ctx context.Context, event TrackingEvent) {
	s.cursorPositions.Record(ctx, int64(event.CursorX), metric.WithAttributes(
		attribute.String("coordinate", "x"),
		attribute.String("event_type", "mousemove"),
	))
	s.cursorPositions.Record(ctx, int64(event.CursorY), metric.WithAttributes(
		attribute.String("coordinate", "y"),
		attribute.String("event_type", "mousemove"),
	))
}

func (s *Service) recordScrollEvent(ctx context.Context, event TrackingEvent) {
	s.cursorPositions.Record(ctx, int64(event.ScrollX), metric.WithAttributes(
		attribute.String("coordinate", "scroll_x"),
		attribute.String("event_type", "scroll"),
	))
	s.cursorPositions.Record(ctx, int64(event.ScrollY), metric.WithAttributes(
		attribute.String("coordinate", "scroll_y"),
		attribute.String("event_type", "scroll"),
	))
}

func (s *Service) recordCustomEvent(ctx context.Context, event TrackingEvent) {
	// Create custom span for analytics
	_, customSpan := s.tracer.Start(ctx, "custom_event")
	customSpan.SetAttributes(
		attribute.String("custom.element_id", event.ElementID),
		attribute.String("custom.session_id", event.SessionID),
	)

	// Add custom attributes if present
	for key, value := range event.Custom {
		if str, ok := value.(string); ok {
			customSpan.SetAttributes(attribute.String("custom."+key, str))
		}
	}

	customSpan.End()
}

func (s *Service) updateSession(event TrackingEvent) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	session, exists := s.sessions[event.SessionID]
	if !exists {
		// Create new session
		session = &SessionData{
			ID:         event.SessionID,
			StartTime:  time.Now(),
			LastActive: time.Now(),
			ClickCount: 0,
			Events:     make([]TrackingEvent, 0),
		}
		s.sessions[event.SessionID] = session
		atomic.AddInt64(&s.sessionCounter, 1)
	}

	// Update session
	session.LastActive = time.Now()
	session.Events = append(session.Events, event)

	if event.EventType == "click" {
		session.ClickCount++
	}
}

func (s *Service) TrackPageView(ctx context.Context) {
	atomic.AddInt64(&s.pageViews, 1)

	_, span := s.tracer.Start(ctx, "page_view")
	span.SetAttributes(
		attribute.Int64("page_views.total", atomic.LoadInt64(&s.pageViews)),
		attribute.String("page.type", "homepage"),
	)
	span.End()

	slog.Info("Page view recorded", "total_views", atomic.LoadInt64(&s.pageViews))
}

func (s *Service) RecordHTTPMetrics(ctx context.Context, method, path string, statusCode int, duration time.Duration) {
	// Record HTTP request counter
	s.httpRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("path", path),
		attribute.Int("status_code", statusCode),
	))

	// Record request duration
	s.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("path", path),
		attribute.Int("status_code", statusCode),
	))
}

func (s *Service) GetHealthMetrics(ctx context.Context) HealthMetrics {
	s.sessionMutex.RLock()
	activeUsers := int64(len(s.sessions))
	s.sessionMutex.RUnlock()

	uptime := time.Since(s.startTime)

	return HealthMetrics{
		Uptime:        uptime.String(),
		TotalClicks:   atomic.LoadInt64(&s.clickCounter),
		PageViews:     atomic.LoadInt64(&s.pageViews),
		ActiveUsers:   activeUsers,
		TotalSessions: atomic.LoadInt64(&s.sessionCounter),
	}
}

func (s *Service) GetClickRate(duration time.Duration) float64 {
	clicks := atomic.LoadInt64(&s.clickCounter)
	minutes := duration.Minutes()
	if minutes == 0 {
		return 0
	}
	return float64(clicks) / minutes
}

func (s *Service) CleanupOldSessions(maxAge time.Duration) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	now := time.Now()
	for sessionID, session := range s.sessions {
		if now.Sub(session.LastActive) > maxAge {
			delete(s.sessions, sessionID)
		}
	}
}
