package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Tracing is off unless an OTLP endpoint is set, and OTEL_SDK_DISABLED wins (M60).
func TestSetupTracing(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	before := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(before) })
	for name, env := range map[string]map[string]string{
		"unset":    {},
		"disabled": {"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318", "OTEL_SDK_DISABLED": "TRUE"},
	} {
		stop, err := setupTracing(ctx, func(k string) string { return env[k] }, log)
		if err != nil || stop(ctx) != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, on := otel.GetTracerProvider().(*sdktrace.TracerProvider); on {
			t.Fatalf("%s: tracing switched on", name)
		}
	}
	env := map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://127.0.0.1:1/v1/traces", "OTEL_SERVICE_NAME": "inv-test"}
	stop, err := setupTracing(ctx, func(k string) string { return env[k] }, log)
	if err != nil {
		t.Fatal(err)
	}
	if _, on := otel.GetTracerProvider().(*sdktrace.TracerProvider); !on {
		t.Fatal("tracing not on with an endpoint")
	}
	_ = stop(ctx) // nothing listens there: flushing may fail, stopping must not hang
	if serviceName(func(string) string { return "" }) != "cactusinv" {
		t.Fatal("default service name")
	}
}
