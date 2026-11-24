package httpapi

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// withTracing starts an OpenTelemetry server span per request (M60), continuing a trace
// the caller sent (traceparent). Without a tracer provider (OTEL_EXPORTER_OTLP_ENDPOINT
// unset) the global one does nothing. Probes, /metrics and the customers' link pages are
// not traced: a span records the path, and theirs carry the link's token.
func withTracing(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http",
		otelhttp.WithFilter(func(r *http.Request) bool {
			switch r.URL.Path {
			case "/healthz", "/readyz", "/metrics":
				return false
			}
			return redactPath(r.URL.Path) == r.URL.Path
		}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
				return r.Method + " /api" // renamed to the operation once it is known
			}
			return r.Method + " web"
		}),
	)
}

// traceOperation names the request's span after the API operation (e.g. "GetInvoice"),
// never the path: paths hold IDs and tokens.
func traceOperation(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		if span := trace.SpanFromContext(ctx); span.IsRecording() {
			span.SetName(operationID)
			span.SetAttributes(attribute.String("cactusinv.operation", operationID))
		}
		return f(ctx, w, r, request)
	}
}

// tokenPrefixes are the paths whose next segment is a secret link token: the customers'
// quote, invoice and account pages, password resets and their API routes.
var tokenPrefixes = []string{
	"/api/v1/public/quotes/", "/api/v1/public/invoices/", "/api/v1/public/accounts/",
	"/api/v1/auth/password-reset/", "/q/", "/i/", "/a/", "/reset-password/",
}

// redactPath replaces a link token in a path with ":token", for logs.
func redactPath(p string) string {
	for _, pre := range tokenPrefixes {
		if rest, ok := strings.CutPrefix(p, pre); ok && rest != "" {
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				return pre + ":token" + rest[i:]
			}
			return pre + ":token"
		}
	}
	return p
}
