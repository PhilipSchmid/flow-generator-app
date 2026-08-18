package tracing

import (
	"context"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInitTracerCreatesRecordingSpans(t *testing.T) {
	logging.InitLogger("json", "error")
	tp, err := InitTracer(context.Background(), "test-service", "http://localhost:4317")
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = Shutdown(ctx, tp)
	})

	_, span := otel.Tracer("test-service").Start(context.Background(), "test-operation")
	assert.True(t, span.IsRecording())
	span.End()
	assert.True(t, Enabled())
}

func TestInitTracerRejectsInvalidEndpoints(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:4317", "ftp://localhost:4317", "http://"} {
		t.Run(endpoint, func(t *testing.T) {
			tp, err := InitTracer(context.Background(), "test-service", endpoint)
			require.Error(t, err)
			require.Nil(t, tp)
		})
	}
}

func TestShutdownWithoutProvider(t *testing.T) {
	require.NoError(t, Shutdown(context.Background(), nil))
	assert.False(t, Enabled())
}

func TestTracerWithNoopProvider(t *testing.T) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	tracer := otel.Tracer("noop-test")
	_, span := tracer.Start(context.Background(), "noop-operation")
	assert.False(t, span.IsRecording())
}

func BenchmarkTracerSpanCreation(b *testing.B) {
	logging.InitLogger("json", "error")
	tp, err := InitTracer(context.Background(), "bench-service", "http://localhost:4317")
	require.NoError(b, err)
	b.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = Shutdown(ctx, tp)
	})
	tracer := otel.Tracer("bench-service")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, span := tracer.Start(context.Background(), "bench-operation")
		span.End()
	}
}
