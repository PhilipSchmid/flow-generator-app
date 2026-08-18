package tracing

import (
	"context"
	"fmt"
	"net/url"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
)

var enabled atomic.Bool

// InitTracer configures an OTLP/gRPC exporter. The endpoint URL controls
// transport security: use https:// for TLS and http:// for plaintext.
func InitTracer(ctx context.Context, serviceName, endpoint string) (*sdktrace.TracerProvider, error) {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid OTLP endpoint %q: expected http(s)://host:port", endpoint)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("initialize tracing exporter: %w", err)
	}

	// Set up the tracer provider with the exporter and resource
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
		)),
	)

	// Set the global tracer provider
	otel.SetTracerProvider(tp)
	enabled.Store(true)

	logging.Logger.Infof("OpenTelemetry tracing enabled for %s via %s", serviceName, endpoint)
	return tp, nil
}

// Enabled reports whether application hot paths should create spans.
func Enabled() bool {
	return enabled.Load()
}

// Shutdown flushes pending spans and disables application instrumentation.
func Shutdown(ctx context.Context, tp *sdktrace.TracerProvider) error {
	enabled.Store(false)
	if tp == nil {
		return nil
	}
	if err := tp.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown tracer provider: %w", err)
	}
	return nil
}
