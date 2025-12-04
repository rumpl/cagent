package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestTracingProvider_NilTracer(t *testing.T) {
	provider := newTracingProvider(nil)

	ctx := t.Context()
	newCtx, span := provider.StartSpan(ctx, "test-span")

	assert.Equal(t, ctx, newCtx, "expected same context when tracer is nil")

	span.End()
	span.SetName("renamed")
	span.RecordError(nil)
}

func TestTracingProvider_NilProvider(t *testing.T) {
	var provider *tracingProvider

	ctx := t.Context()
	newCtx, span := provider.StartSpan(ctx, "test-span")

	assert.Equal(t, ctx, newCtx, "expected same context when provider is nil")

	span.End()
}

func TestTracingProvider_WithTracer(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	provider := newTracingProvider(tracer)

	ctx := t.Context()
	newCtx, span := provider.StartSpan(ctx, "test-span")

	assert.NotEqual(t, ctx, newCtx, "expected new context when tracer is configured")

	spanFromCtx := trace.SpanFromContext(newCtx)
	require.Equal(t, spanFromCtx, span, "span should be in returned context")

	span.End()
}

func TestTracingProvider_HasTracer(t *testing.T) {
	nilProvider := newTracingProvider(nil)
	assert.False(t, nilProvider.HasTracer(), "expected HasTracer() to return false for nil tracer")

	tracer := noop.NewTracerProvider().Tracer("test")
	provider := newTracingProvider(tracer)
	assert.True(t, provider.HasTracer(), "expected HasTracer() to return true")
}

func TestTracingProvider_SetTracer(t *testing.T) {
	provider := newTracingProvider(nil)

	assert.False(t, provider.HasTracer(), "expected no tracer initially")

	tracer := noop.NewTracerProvider().Tracer("test")
	provider.SetTracer(tracer)

	assert.True(t, provider.HasTracer(), "expected tracer after SetTracer")
}
