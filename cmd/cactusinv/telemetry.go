package main

import (
	"context"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

// setupTracing sends OpenTelemetry traces (M60) over OTLP/HTTP when the standard
// OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT is set (and
// OTEL_SDK_DISABLED is not true); the exporter reads the other OTEL_* settings (headers,
// timeout, sampling via OTEL_TRACES_SAMPLER). Without them it does nothing. It returns a
// function that flushes and stops the exporter at shutdown.
func setupTracing(ctx context.Context, getenv func(string) string, log *slog.Logger) (func(context.Context) error, error) {
	off := func(context.Context) error { return nil }
	if strings.EqualFold(strings.TrimSpace(getenv("OTEL_SDK_DISABLED")), "true") ||
		(getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "") {
		return off, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return off, err
	}
	// OTEL_RESOURCE_ATTRIBUTES adds to these; OTEL_SERVICE_NAME names the service.
	res, err := resource.New(ctx, resource.WithTelemetrySDK(), resource.WithHost(), resource.WithFromEnv(),
		resource.WithAttributes(semconv.ServiceName(serviceName(getenv)), semconv.ServiceVersion(version)))
	if err != nil {
		return off, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	log.Info("tracing on", "service", serviceName(getenv))
	return tp.Shutdown, nil
}

func serviceName(getenv func(string) string) string {
	if n := strings.TrimSpace(getenv("OTEL_SERVICE_NAME")); n != "" {
		return n
	}
	return "cactusinv"
}
