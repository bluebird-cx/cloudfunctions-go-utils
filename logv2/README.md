# logv2 (Cloud Logging client)

A small, reusable Google Cloud Logging client for Go. It reuses a single `logging.Client`, supports request correlation (trace and execution ID), structured payloads, and optional notifier hooks.

## Features

- Single client reused across calls (no per-call client creation)
- Functional options: ProjectID, LogName, Invoker, CommonLabels, ExecutionID header keys
- Request correlation: extracts trace from `X-Cloud-Trace-Context` and execution ID from `Function-Execution-Id`
- Request-scoped logger (`ForRequest`) so you don't pass `ctx`/`*http.Request` every time
- Fallback to structured JSON on stdout when running locally or when project ID is not available
- Optional notifier hook for errors and above

## Install

This package is part of `cloudfunctions-go-utils`. Import:

```go
import v2 "github.com/bluebird-cx/cloudfunctions-go-utils/logv2"
```

## Quick start

```go
package main

import (
    v2 "github.com/bluebird-cx/cloudfunctions-go-utils/logv2"
    "cloud.google.com/go/logging"
    "context"
    "fmt"
    "net/http"
    "os"
)

func main() {
    ctx := context.Background()

    lg, err := v2.New(ctx,
        v2.WithInvoker("ieos-server"),
        v2.WithLogName("IEOS_Backend"),
        // v2.WithProjectID(os.Getenv("GOOGLE_CLOUD_PROJECT")), // optional; auto-detected if omitted
        v2.WithCommonLabels(map[string]string{
            "service": "ieos",
            "env":     os.Getenv("ENVIRONMENT"),
        }),
        // v2.WithNotifier(mySlackNotifier{}, logging.Error), // optional
    )
    if err != nil { panic(err) }
    defer lg.Close()

    // Option A: pass ctx/req explicitly when needed
    lg.Info(ctx, (*http.Request)(nil), "server started")

    // Option B: bind per-request once and use a request-scoped logger
    req := (*http.Request)(nil) // replace with real request
    reqLog := lg.ForRequest(ctx, req)
    reqLog.Info("user signed in", map[string]any{"userId": "123"})
    // Passing error values is supported; they are stringified in the payload
    reqLog.Error("failed to create invoice", fmt.Errorf("boom"))

    // Severity thresholded notifier example
    lg.Error(ctx, req, "important failure", map[string]any{"orderId": 42})
}

// Example notifier implementation

type mySlackNotifier struct{}

func (mySlackNotifier) Notify(ctx context.Context, severity logging.Severity, executionID string, message string, payload any) {
    // send message to Slack (implementation omitted)
}
```

## API

- `New(ctx, ...Option) (*CloudLogger, error)`
- Options:
  - `WithProjectID(string)`
  - `WithLogName(string)`
  - `WithInvoker(string)`
  - `WithCommonLabels(map[string]string)`
  - `WithExecutionIDHeaders(...string)`
  - `WithNotifier(Notifier, logging.Severity)`
  - `WithStdoutOnly()`
- Methods:
  - `Debug/Info/Notice/Warning/Error/Critical/Emergency(ctx, *http.Request, message string, data ...any)`
  - `ForRequest(ctx, *http.Request) *RequestLogger`
  - `Close() error`

## Notes

- Call `Close()` on shutdown to flush buffered Cloud Logging entries.
- When running locally without `GOOGLE_CLOUD_PROJECT`, the logger falls back to stdout so you can still see JSON logs.
- You can keep using the existing `logger.go` in this repository. `logv2` is a new, optional API you can migrate to gradually.
