package cloudfunctions_go_utils

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"cloud.google.com/go/logging"
)

// Logger - the main model
// LoggerInvoker - the name of the log invoker. Used to distinguish system logs and custom logs
type Logger struct {
	ProjectID     string
	LoggerInvoker string
	LogName       string
}

func NewLogger(projectID string, loggerInvoker string, logName string) *Logger {
	return &Logger{
		ProjectID:     projectID,
		LoggerInvoker: loggerInvoker,
		LogName:       logName,
	}
}

// LogEntryPayload - used as a data model for Log Entry payload
type LogEntryPayload struct {
	Invoker     string        `json:"invoker"`
	Message     string        `json:"message"`
	ExecutionID string        `json:"execution_id"`
	DataObject  []interface{} `json:"data_object"`
}

func (pl *Logger) getExecutionFunctionIDFromRequest(httpRequest *http.Request) string {
	if httpRequest != nil {
		return httpRequest.Header.Get("Function-Execution-Id")
	}

	return ""
}

func (pl *Logger) getTraceId(httpRequest *http.Request) string {
	var trace string
	if pl.ProjectID != "" && httpRequest != nil {
		traceHeader := httpRequest.Header.Get("X-Cloud-Trace-Context")
		traceParts := strings.Split(traceHeader, "/")
		if len(traceParts) > 0 && len(traceParts[0]) > 0 {
			trace = fmt.Sprintf("projects/%s/traces/%s", pl.ProjectID, traceParts[0])
		}
	}

	return trace
}

// Debug - calls sendLogs with 100(DEBUG) severity. Used for BE team
func (pl *Logger) Debug(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	pl.sendLogs(ctx, logging.Debug, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, pl.getTraceId(httpRequest))
}

// Info - calls sendLogs with 200(INFO) severity. Used for BE team
func (pl *Logger) Info(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	pl.sendLogs(ctx, logging.Info, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, pl.getTraceId(httpRequest))
}

// Notice - calls sendLogs with 300(NOTICE) severity. Used for Support team
func (pl *Logger) Notice(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	pl.sendLogs(ctx, logging.Notice, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, pl.getTraceId(httpRequest))
}

// Warning - calls sendLogs with 400(WARNING) severity. Like Error2 informal (the flow is not stopped)
func (pl *Logger) Warning(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	pl.sendLogs(ctx, logging.Warning, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, pl.getTraceId(httpRequest))
}

// Error - calls sendLogs with 500(ERROR) severity. Like Error2 that stops the flow
func (pl *Logger) Error(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	pl.sendLogs(ctx, logging.Error, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, pl.getTraceId(httpRequest))
}

// Critical - calls sendLogs with 600(CRITICAL) severity. Like Error1 with fix time 24 hours
func (pl *Logger) Critical(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	pl.sendLogs(ctx, logging.Critical, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, pl.getTraceId(httpRequest))
}

// Emergency - calls sendLogs with 800(EMERGENCY) severity. Like Error1 P0 that should be fixed ASAP
func (pl *Logger) Emergency(ctx context.Context, httpRequest *http.Request, message string, dataObject ...interface{}) {
	pl.sendLogs(ctx, logging.Emergency, LogEntryPayload{
		Invoker:     pl.LoggerInvoker,
		Message:     message,
		ExecutionID: pl.getExecutionFunctionIDFromRequest(httpRequest),
		DataObject:  dataObject,
	}, pl.getTraceId(httpRequest))
}

// sendLogs - the main business logic function used in other high-level functions
func (pl *Logger) sendLogs(ctx context.Context, severity logging.Severity, payload LogEntryPayload, trace string) {
	client, err := logging.NewClient(ctx, pl.ProjectID)
	if err != nil {
		log.Fatalf("Failed to create logging client: %v", err)
	}
	defer client.Close()
	logger := client.Logger(pl.LogName)
	defer logger.Flush() // Ensure the entry is written.

	logger.Log(logging.Entry{
		// Log anything that can be marshaled to JSON.
		Payload:  payload,
		Severity: severity,
		Trace:    trace,
	})
}
