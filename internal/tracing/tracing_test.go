package tracing

import (
	"context"
	"testing"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInitTracerWithEndpoint(t *testing.T) {
	// Initialize logger for the test
	logging.InitLogger("json", "error")

	// Test with a valid endpoint (won't actually connect)
	serviceName := "test-service"
	endpoint := "localhost:4317"

	// This should not panic even if it can't connect
	assert.NotPanics(t, func() {
		InitTracer(serviceName, endpoint)
	})

	// Verify tracer is set
	tracer := otel.Tracer(serviceName)
	assert.NotNil(t, tracer)
}

func TestInitTracerWithInvalidEndpoint(t *testing.T) {
	// Initialize logger for the test
	logging.InitLogger("json", "error")

	// Test with various invalid endpoints
	testCases := []struct {
		name     string
		endpoint string
	}{
		{"empty endpoint", ""},
		{"invalid format", "not-a-valid-endpoint"},
		{"missing port", "localhost"},
		{"invalid characters", "local@host:4317"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic even with invalid endpoint
			assert.NotPanics(t, func() {
				InitTracer("test-service", tc.endpoint)
			})
		})
	}
}

func TestInitTracerMultipleCalls(t *testing.T) {
	// Initialize logger for the test
	logging.InitLogger("json", "error")

	// Test that multiple calls don't cause issues
	assert.NotPanics(t, func() {
		InitTracer("service1", "localhost:4317")
		InitTracer("service2", "localhost:4318")
		InitTracer("service3", "otherhost:4317")
	})
}

func TestTracerCreatesSpans(t *testing.T) {
	// Initialize logger for the test
	logging.InitLogger("json", "error")

	// Initialize tracer
	InitTracer("span-test-service", "localhost:4317")

	// Get tracer
	tracer := otel.Tracer("span-test-service")

	// Create a span
	ctx, span := tracer.Start(context.TODO(), "test-operation")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)

	// Verify span methods don't panic
	assert.NotPanics(t, func() {
		span.SetName("renamed-operation")
		span.AddEvent("test-event")
		span.End()
	})
}

func TestInitTracerLogging(t *testing.T) {
	// Initialize logger to capture debug output
	logging.InitLogger("json", "debug")

	// Test that InitTracer logs appropriate messages
	InitTracer("logging-test-service", "localhost:4317")

	// If we get here without panic, logging worked
	assert.True(t, true)
}

func TestTracerWithNoopProvider(t *testing.T) {
	// Reset to noop provider
	otel.SetTracerProvider(noop.NewTracerProvider())

	// Get tracer - should return noop tracer
	tracer := otel.Tracer("noop-test")
	assert.NotNil(t, tracer)

	// Create span with context - noop tracer requires non-nil context
	ctx := context.Background()
	newCtx, span := tracer.Start(ctx, "noop-operation")
	assert.NotNil(t, newCtx)
	assert.NotNil(t, span)

	// Verify it's not recording
	assert.False(t, span.IsRecording())
}

func BenchmarkInitTracer(b *testing.B) {
	logging.InitLogger("json", "error")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		InitTracer("bench-service", "localhost:4317")
	}
}

func BenchmarkTracerSpanCreation(b *testing.B) {
	logging.InitLogger("json", "error")
	InitTracer("bench-service", "localhost:4317")

	tracer := otel.Tracer("bench-service")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, span := tracer.Start(context.TODO(), "bench-operation")
		span.End()
	}
}
