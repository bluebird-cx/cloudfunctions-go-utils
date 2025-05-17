package cloudfunctions_go_utils

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"cloud.google.com/go/logging"
	"go.opentelemetry.io/otel/attribute" // For span attributes
	"go.opentelemetry.io/otel/trace"     // For OpenTelemetry trace and span
)

// Logger - the main model
type Logger struct {
	ProjectID     string
	LoggerInvoker string
	LogName       string
	loggingClient *logging.Client // Reusable Cloud Logging client
}

// NewLogger initializes a new Logger with a Cloud Logging client.
// It's recommended to create one Logger instance and reuse it, or ensure Close() is called if created per request.
func NewLogger(projectID string, loggerInvoker string, logName string) (*Logger, error) {
	ctx := context.Background() // Use background context for client initialization
	client, err := logging.NewClient(ctx, fmt.Sprintf("projects/%s", projectID))
	if err != nil {
		return nil, fmt.Errorf("failed to create logging client: %w", err)
	}
	return &Logger{
		ProjectID:     projectID,
		LoggerInvoker: loggerInvoker,
		LogName:       logName,
		loggingClient: client,
	}, nil
}

func (pl *Logger) Close() error {
	if pl.loggingClient != nil {
		var errFlush error
		// Get the specific logger instance for pl.LogName to flush it
		if pl.LogName != "" { // Ensure LogName is set before trying to get a logger for it
			gcpLogger := pl.loggingClient.Logger(pl.LogName)
			errFlush = gcpLogger.Flush() // Flush logs for this specific logger
		}

		errClose := pl.loggingClient.Close() // Then close the client

		if errFlush != nil {
			// Report flush error, and include client close error if it also occurred
			if errClose != nil {
				return fmt.Errorf("failed to flush logger '%s': %w; also failed to close client: %v", pl.LogName, errFlush, errClose)
			}
			return fmt.Errorf("failed to flush logger '%s': %w", pl.LogName, errFlush)
		}
		// If flush was successful (or not performed due to empty LogName), return any client close error
		return errClose
	}
	return nil
}

// LogEntryPayload - used as a data model for Log Entry payload
type LogEntryPayload struct {
	Invoker     string        `json:"invoker"`
	Message     string        `json:"message"`
	ExecutionID string        `json:"execution_id,omitempty"`
	DataObject  []interface{} `json:"data_object,omitempty"`
}

func (pl *Logger) getExecutionFunctionIDFromRequest(httpRequest *http.Request) string {
	if httpRequest != nil {
		return httpRequest.Header.Get("Function-Execution-Id")
	}
	return ""
}

// extractTraceSpanInfo extracts trace ID, span ID, and sampling decision.
// It prioritizes an active OpenTelemetry span from the context.
// If no OTel span, it falls back to the X-Cloud-Trace-Context header for the trace ID and sampling decision.
func (pl *Logger) extractTraceSpanInfo(ctx context.Context, httpRequest *http.Request) (traceID, spanID string, sampled bool) {
	// Attempt to get span from OpenTelemetry context
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		traceID = fmt.Sprintf("projects/%s/traces/%s", pl.ProjectID, span.SpanContext().TraceID().String())
		spanID = span.SpanContext().SpanID().String()
		sampled = span.SpanContext().IsSampled()
		return
	}

	// Fallback to X-Cloud-Trace-Context header if no OTel span and ProjectID is available.
	if httpRequest != nil && pl.ProjectID != "" {
		traceHeader := httpRequest.Header.Get("X-Cloud-Trace-Context")
		if traceHeader != "" {
			parts := strings.Split(traceHeader, "/")
			if len(parts) > 0 && len(parts[0]) > 0 {
				traceID = fmt.Sprintf("projects/%s/traces/%s", pl.ProjectID, parts[0])
				// Check for sampling decision in the header (e.g., ";o=1" for sampled)
				if strings.Contains(traceHeader, ";o=1") {
					sampled = true
				} else if strings.Contains(traceHeader, ";o=0") {
					sampled = false
				}
			}
		}
	}
	return
}

// sendLogs - the main business logic function used in other high-level functions
func (pl *Logger) sendLogs(ctx context.Context, severity logging.Severity, payload LogEntryPayload, traceID, spanID string, traceSampled bool) {
	// Add log message as an event to the current OpenTelemetry span, if one exists and is recording.
	currentSpan := trace.SpanFromContext(ctx)
	if currentSpan.IsRecording() {
		attrs := []attribute.KeyValue{
			attribute.String("log.message", payload.Message),
			attribute.String("log.severity", severity.String()),
			attribute.String("log.invoker", payload.Invoker),
		}
		if payload.ExecutionID != "" {
			attrs = append(attrs, attribute.String("log.execution_id", payload.ExecutionID))
		}
		currentSpan.AddEvent("log", trace.WithAttributes(attrs...))
	}

	// Use the pre-initialized logger client
	gcpLogger := pl.loggingClient.Logger(pl.LogName)

	entry := logging.Entry{
		Payload:  payload,
		Severity: severity,
	}
	if traceID != "" {
		entry.Trace = traceID
	}
	if spanID != "" {
		entry.SpanID = spanID
	}
	entry.TraceSampled = traceSampled // Set based on OTel span or header

	gcpLogger.Log(entry)
}

// Debug - calls sendLogs with 100(DEBUG) severity. Used for BE team
func (pl *Logger) Debug(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	traceID, spanID, sampled := pl.extractTraceSpanInfo(ctx, httpRequest)
	pl.sendLogs(ctx, logging.Debug, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, traceID, spanID, sampled)
}

// Info - calls sendLogs with 200(INFO) severity. Used for BE team
func (pl *Logger) Info(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	traceID, spanID, sampled := pl.extractTraceSpanInfo(ctx, httpRequest)
	pl.sendLogs(ctx, logging.Info, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, traceID, spanID, sampled)
}

// Notice - calls sendLogs with 300(NOTICE) severity. Used for Support team
func (pl *Logger) Notice(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	traceID, spanID, sampled := pl.extractTraceSpanInfo(ctx, httpRequest)
	pl.sendLogs(ctx, logging.Notice, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, traceID, spanID, sampled)
}

// Warning - calls sendLogs with 400(WARNING) severity. Like Error2 informal (the flow is not stopped)
func (pl *Logger) Warning(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	traceID, spanID, sampled := pl.extractTraceSpanInfo(ctx, httpRequest)
	pl.sendLogs(ctx, logging.Warning, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, traceID, spanID, sampled)
}

// Error - calls sendLogs with 500(ERROR) severity. Like Error2 that stops the flow
func (pl *Logger) Error(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	traceID, spanID, sampled := pl.extractTraceSpanInfo(ctx, httpRequest)
	pl.sendLogs(ctx, logging.Error, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, traceID, spanID, sampled)
}

// Critical - calls sendLogs with 600(CRITICAL) severity. Like Error1 with fix time 24 hours
func (pl *Logger) Critical(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	traceID, spanID, sampled := pl.extractTraceSpanInfo(ctx, httpRequest)
	pl.sendLogs(ctx, logging.Critical, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, traceID, spanID, sampled)
}

// Emergency - calls sendLogs with 800(EMERGENCY) severity. Like Error1 P0 that should be fixed ASAP
func (pl *Logger) Emergency(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	traceID, spanID, sampled := pl.extractTraceSpanInfo(ctx, httpRequest)
	pl.sendLogs(ctx, logging.Emergency, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, traceID, spanID, sampled)
}
