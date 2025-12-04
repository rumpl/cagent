package runtime

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

type tracingProvider struct {
	tracer trace.Tracer
}

func newTracingProvider(tracer trace.Tracer) *tracingProvider {
	return &tracingProvider{tracer: tracer}
}

func (t *tracingProvider) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t == nil || t.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return t.tracer.Start(ctx, name, opts...)
}

func (t *tracingProvider) SetTracer(tracer trace.Tracer) {
	if t != nil {
		t.tracer = tracer
	}
}

func (t *tracingProvider) HasTracer() bool {
	return t != nil && t.tracer != nil
}
