package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
)

func InitTracer(serviceName, endpoint string) {
	// Create the OTLP gRPC exporter
	exporter, err := otlptracegrpc.New(context.Background(), otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
	if err != nil {
		logging.Logger.Warnf("Failed to initialize tracing exporter: %v", err)
		return
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

	logging.Logger.Debugf("Tracing initialized for service %s with endpoint %s", serviceName, endpoint)
}
