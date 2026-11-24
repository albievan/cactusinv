package httpapi_test

import (
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// With a tracer provider each API request is one server span named after its operation
// (M60); the customers' link pages, holding a token in the path, are not traced.
func TestTracingNamesSpansAfterOperations(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	before := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(before) })

	c, _, _, _ := newServer(t)
	c.login()
	c.ok(c.do("GET", "/api/v1/auth/me", nil, nil), 200, "me")
	c.ok(c.do("GET", "/api/v1/public/accounts/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", nil, nil), 404, "account link")
	resp, err := c.get("/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var names []string
	for _, s := range rec.Ended() {
		if s.SpanKind().String() == "server" {
			names = append(names, s.Name())
		}
	}
	if len(names) != 2 || names[0] != "Login" || names[1] != "Me" {
		t.Fatalf("server spans = %q", names)
	}
}
