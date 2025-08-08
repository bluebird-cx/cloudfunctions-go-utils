package logv2

import (
	cloudmeta "cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/logging"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

type Notifier interface {
	Notify(ctx context.Context, severity logging.Severity, executionID string, message string, payload any)
}

type Options struct {
	ProjectID             string
	LogName               string
	Invoker               string
	CommonLabels          map[string]string
	ExecutionIDHeaderKeys []string
	NotifyMinSeverity     logging.Severity
	Hook                  Notifier
	ForceStdout           bool
}

type Option func(*Options)

func WithProjectID(id string) Option            { return func(o *Options) { o.ProjectID = id } }
func WithLogName(name string) Option            { return func(o *Options) { o.LogName = name } }
func WithInvoker(invoker string) Option         { return func(o *Options) { o.Invoker = invoker } }
func WithCommonLabels(labels map[string]string) Option {
	return func(o *Options) { o.CommonLabels = labels }
}
func WithExecutionIDHeaders(keys ...string) Option {
	return func(o *Options) { o.ExecutionIDHeaderKeys = append([]string{}, keys...) }
}
func WithNotifier(h Notifier, min logging.Severity) Option {
	return func(o *Options) { o.Hook = h; o.NotifyMinSeverity = min }
}
func WithStdoutOnly() Option { return func(o *Options) { o.ForceStdout = true } }

type CloudLogger struct {
	opts     Options
	client   *logging.Client
	logger   *logging.Logger
	initOnce sync.Once
	initErr  error
}

func New(ctx context.Context, opts ...Option) (*CloudLogger, error) {
	options := Options{
		LogName:               "application",
		ExecutionIDHeaderKeys: []string{"Function-Execution-Id", "X-Cloud-Function-Execution-Id"},
		NotifyMinSeverity:     logging.Error,
	}
	for _, f := range opts {
		f(&options)
	}
	if options.ProjectID == "" && !options.ForceStdout {
		id, err := detectProjectID(ctx)
		if err != nil {
			// fallback to stdout when no project ID
			options.ForceStdout = true
		} else {
			options.ProjectID = id
		}
	}

	cl := &CloudLogger{opts: options}
	cl.initOnce.Do(func() {
		if cl.opts.ForceStdout {
			return
		}
		client, err := logging.NewClient(ctx, cl.opts.ProjectID)
		if err != nil {
			cl.initErr = err
			return
		}
		client.OnError = func(e error) {
			log.Printf("cloud logging async error: %v", e)
		}
		cl.client = client
		lopts := []logging.LoggerOption{}
		if len(cl.opts.CommonLabels) > 0 {
			lopts = append(lopts, logging.CommonLabels(cl.opts.CommonLabels))
		}
		cl.logger = client.Logger(cl.opts.LogName, lopts...)
	})
	return cl, cl.initErr
}

func (c *CloudLogger) Close() error {
	if c.client == nil {
		return nil
	}
	c.logger.Flush()
	return c.client.Close()
}

type LogEntryPayload struct {
	Invoker     string        `json:"invoker,omitempty"`
	Message     string        `json:"message"`
	ExecutionID string        `json:"execution_id,omitempty"`
	DataObject  []interface{} `json:"data_object,omitempty"`
}

func (c *CloudLogger) Debug(ctx context.Context, r *http.Request, message string, data ...interface{}) {
	c.log(ctx, logging.Debug, r, message, data...)
}

func (c *CloudLogger) Info(ctx context.Context, r *http.Request, message string, data ...interface{}) {
	c.log(ctx, logging.Info, r, message, data...)
}

func (c *CloudLogger) Notice(ctx context.Context, r *http.Request, message string, data ...interface{}) {
	c.log(ctx, logging.Notice, r, message, data...)
}

func (c *CloudLogger) Warning(ctx context.Context, r *http.Request, message string, data ...interface{}) {
	c.log(ctx, logging.Warning, r, message, data...)
}

func (c *CloudLogger) Error(ctx context.Context, r *http.Request, message string, data ...interface{}) {
	c.log(ctx, logging.Error, r, message, data...)
}

func (c *CloudLogger) Critical(ctx context.Context, r *http.Request, message string, data ...interface{}) {
	c.log(ctx, logging.Critical, r, message, data...)
}

func (c *CloudLogger) Emergency(ctx context.Context, r *http.Request, message string, data ...interface{}) {
	c.log(ctx, logging.Emergency, r, message, data...)
}

// Request-scoped sugar to avoid passing ctx & req on each call

type RequestLogger struct {
	base *CloudLogger
	ctx  context.Context
	req  *http.Request
}

func (c *CloudLogger) ForRequest(ctx context.Context, r *http.Request) *RequestLogger {
	return &RequestLogger{base: c, ctx: ctx, req: r}
}

func (rl *RequestLogger) Debug(msg string, data ...any)     { rl.base.log(rl.ctx, logging.Debug, rl.req, msg, data...) }
func (rl *RequestLogger) Info(msg string, data ...any)      { rl.base.log(rl.ctx, logging.Info, rl.req, msg, data...) }
func (rl *RequestLogger) Notice(msg string, data ...any)    { rl.base.log(rl.ctx, logging.Notice, rl.req, msg, data...) }
func (rl *RequestLogger) Warning(msg string, data ...any)   { rl.base.log(rl.ctx, logging.Warning, rl.req, msg, data...) }
func (rl *RequestLogger) Error(msg string, data ...any)     { rl.base.log(rl.ctx, logging.Error, rl.req, msg, data...) }
func (rl *RequestLogger) Critical(msg string, data ...any)  { rl.base.log(rl.ctx, logging.Critical, rl.req, msg, data...) }
func (rl *RequestLogger) Emergency(msg string, data ...any) { rl.base.log(rl.ctx, logging.Emergency, rl.req, msg, data...) }

func (c *CloudLogger) log(ctx context.Context, sev logging.Severity, r *http.Request, message string, data ...interface{}) {
	execID := extractExecutionID(r, c.opts.ExecutionIDHeaderKeys)
	trace := extractTrace(c.opts.ProjectID, r)

    normalized := normalizeData(data)

	payload := LogEntryPayload{
		Invoker:     c.opts.Invoker,
		Message:     message,
		ExecutionID: execID,
        DataObject:  normalized,
	}

	if c.client == nil || c.logger == nil || c.opts.ForceStdout {
		writeStdout(sev, payload, c.opts.CommonLabels, trace)
	} else {
		c.logger.Log(logging.Entry{
			Severity: sev,
			Labels:   mergeLabels(c.opts.CommonLabels, map[string]string{"execution_id": execID}),
			Payload:  payload,
			Trace:    trace,
		})
	}

	if c.opts.Hook != nil && sev >= c.opts.NotifyMinSeverity {
		safeNotify(c.opts.Hook, ctx, sev, execID, message, payload)
	}
}

// normalizeData ensures JSON-friendly payloads. In particular, error values
// are converted to their Error() string, because the default json.Marshal on
// concrete error types usually results in an empty object.
func normalizeData(items []interface{}) []interface{} {
    if len(items) == 0 {
        return nil
    }
    out := make([]interface{}, 0, len(items))
    for _, it := range items {
        switch v := it.(type) {
        case error:
            out = append(out, v.Error())
        default:
            out = append(out, it)
        }
    }
    return out
}

func extractExecutionID(r *http.Request, keys []string) string {
	if r == nil {
		return ""
	}
	for _, k := range keys {
		if v := r.Header.Get(k); v != "" {
			return v
		}
	}
	return ""
}

func extractTrace(projectID string, r *http.Request) string {
	if r == nil || projectID == "" {
		return ""
	}
	// X-Cloud-Trace-Context: TRACE_ID/SPAN_ID;o=TRACE_TRUE
	traceHeader := r.Header.Get("X-Cloud-Trace-Context")
	if traceHeader == "" {
		return ""
	}
	parts := strings.Split(traceHeader, "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return fmt.Sprintf("projects/%s/traces/%s", projectID, parts[0])
}

func detectProjectID(ctx context.Context) (string, error) {
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		return v, nil
	}
	if cloudmeta.OnGCE() {
		return cloudmeta.ProjectIDWithContext(ctx)
	}
	return "", errors.New("GOOGLE_CLOUD_PROJECT not set and not on GCE")
}

func writeStdout(sev logging.Severity, payload LogEntryPayload, labels map[string]string, trace string) {
	entry := struct {
		Severity string            `json:"severity"`
		Labels   map[string]string `json:"labels,omitempty"`
		Trace    string            `json:"trace,omitempty"`
		Payload  LogEntryPayload   `json:"payload"`
	}{
		Severity: sev.String(),
		Labels:   mergeLabels(labels, map[string]string{"execution_id": payload.ExecutionID}),
		Trace:    trace,
		Payload:  payload,
	}
	b, _ := json.Marshal(entry)
	log.Println(string(b))
}

func mergeLabels(a, b map[string]string) map[string]string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func safeNotify(n Notifier, ctx context.Context, sev logging.Severity, execID, msg string, payload any) {
	defer func() { _ = recover() }()
	n.Notify(ctx, sev, execID, msg, payload)
}
