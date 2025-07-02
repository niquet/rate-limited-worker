package handlers

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/niquet/rate-limited-worker/internal/service"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Handler struct {
	service *service.Service
	tracer  *trace.Tracer
}

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
	Uptime    string    `json:"uptime"`
}

func New(svc *service.Service) *Handler {
	tracer := otel.Tracer("worker-handlers")
	return &Handler{
		service: svc,
		tracer:  &tracer,
	}
}

func (h *Handler) HomePage(w http.ResponseWriter, r *http.Request) {
	ctx, span := (*h.tracer).Start(r.Context(), "homepage_handler")
	defer span.End()

	// Only serve GET requests to root path
	if r.Method != http.MethodGet {
		span.SetStatus(codes.Error, "method not allowed")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Path != "/" {
		span.SetStatus(codes.Error, "page not found")
		http.NotFound(w, r)
		return
	}

	// Track page view
	h.service.TrackPageView(ctx)

	// Parse and execute template
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Worker - Interactive Tracking Demo</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="container">
        <header class="header">
            <h1 class="title">🔧 Worker Interactive Demo</h1>
            <p class="subtitle">Click anywhere to generate telemetry data!</p>
        </header>
        
        <main class="content">
            <div class="stats-grid">
                <div class="stat-card" id="click-counter">
                    <h3>Total Clicks</h3>
                    <span class="stat-number" id="click-count">0</span>
                </div>
                <div class="stat-card" id="cursor-position">
                    <h3>Cursor Position</h3>
                    <span class="stat-number" id="cursor-coords">0, 0</span>
                </div>
                <div class="stat-card" id="session-time">
                    <h3>Session Time</h3>
                    <span class="stat-number" id="session-timer">0s</span>
                </div>
                <div class="stat-card" id="interaction-rate">
                    <h3>Interaction Rate</h3>
                    <span class="stat-number" id="rate-display">0/min</span>
                </div>
            </div>

            <div class="interactive-area">
                <div class="click-zone" id="zone-1">
                    <h2>Click Zone Alpha</h2>
                    <p>This area tracks detailed click analytics</p>
                </div>
                <div class="click-zone" id="zone-2">
                    <h2>Click Zone Beta</h2>
                    <p>Performance metrics are collected here</p>
                </div>
                <div class="click-zone" id="zone-3">
                    <h2>Click Zone Gamma</h2>
                    <p>User interaction patterns recorded</p>
                </div>
            </div>

            <div class="action-buttons">
                <button class="btn btn-primary" id="generate-event">Generate Custom Event</button>
                <button class="btn btn-secondary" id="clear-stats">Clear Statistics</button>
                <button class="btn btn-accent" id="export-data">Export Data</button>
            </div>
        </main>

        <footer class="footer">
            <p>Powered by OpenTelemetry • Go Backend • Real-time Analytics</p>
            <div class="status-indicator">
                <span class="status-dot"></span>
                <span id="connection-status">Connected</span>
            </div>
        </footer>
    </div>

    <script src="/static/js/tracking.js"></script>
</body>
</html>`

	// Set content type and cache headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Parse template
	t, err := template.New("homepage").Parse(tmpl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "template parsing failed")
		slog.Error("Failed to parse template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Execute template
	if err := t.Execute(w, nil); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "template execution failed")
		slog.Error("Failed to execute template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	span.SetAttributes(
		attribute.String("http.route", "/"),
		attribute.String("response.type", "html"),
	)
	span.SetStatus(codes.Ok, "homepage rendered successfully")
}

func (h *Handler) TrackEvent(w http.ResponseWriter, r *http.Request) {
	ctx, span := (*h.tracer).Start(r.Context(), "track_event_handler")
	defer span.End()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		span.SetStatus(codes.Error, "method not allowed")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse JSON request
	var event service.TrackingEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid JSON")
		slog.Error("Failed to decode tracking event", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate event
	if event.EventType == "" {
		span.SetStatus(codes.Error, "missing event type")
		http.Error(w, "Event type is required", http.StatusBadRequest)
		return
	}

	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Add request metadata
	event.UserAgent = r.UserAgent()
	if event.PageURL == "" {
		event.PageURL = r.Referer()
	}

	// Process event through service layer
	if err := h.service.ProcessTrackingEvent(ctx, event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "event processing failed")
		slog.Error("Failed to process tracking event", "error", err, "event_type", event.EventType)
		http.Error(w, "Failed to process event", http.StatusInternalServerError)
		return
	}

	// Log event details
	slog.Info("Tracking event processed",
		"event_type", event.EventType,
		"cursor_x", event.CursorX,
		"cursor_y", event.CursorY,
		"element_id", event.ElementID,
		"session_id", event.SessionID,
	)

	// Add span attributes
	span.SetAttributes(
		attribute.String("event.type", event.EventType),
		attribute.Int("cursor.x", event.CursorX),
		attribute.Int("cursor.y", event.CursorY),
		attribute.String("element.id", event.ElementID),
		attribute.String("element.type", event.ElementType),
		attribute.String("session.id", event.SessionID),
	)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"status":    "success",
		"timestamp": time.Now(),
		"event_id":  event.SessionID + "_" + event.EventType,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		span.RecordError(err)
		slog.Error("Failed to encode response", "error", err)
	}

	span.SetStatus(codes.Ok, "event tracked successfully")
}

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, span := (*h.tracer).Start(r.Context(), "health_check_handler")
	defer span.End()

	if r.Method != http.MethodGet {
		span.SetStatus(codes.Error, "method not allowed")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get health metrics from service
	healthData := h.service.GetHealthMetrics(ctx)

	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   "v1.0.0",
		Uptime:    healthData.Uptime,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "encoding failed")
		slog.Error("Failed to encode health response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	span.SetStatus(codes.Ok, "health check completed")
}
