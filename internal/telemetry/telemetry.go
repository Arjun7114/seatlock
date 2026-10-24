package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup initializes OpenTelemetry tracing and metrics for the service.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is set (e.g. "http://localhost:4318"),
// telemetry is exported over OTLP/HTTP to that endpoint. Otherwise it is written
// to stdout, so the instrumentation can be verified with no backend running.
//
// It returns a shutdown function that flushes and stops the providers; call it
// on process exit.
func Setup(ctx context.Context) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "seatlock"),
			attribute.String("service.version", "0.1.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// When the standard OTLP endpoint env var is set, export over OTLP (the
	// exporters read OTEL_EXPORTER_OTLP_ENDPOINT themselves); otherwise stdout.
	useOTLP := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""

	var traceExp sdktrace.SpanExporter
	if useOTLP {
		traceExp, err = otlptracehttp.New(ctx)
	} else {
		traceExp, err = stdouttrace.New()
	}
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	var metricExp sdkmetric.Exporter
	if useOTLP {
		metricExp, err = otlpmetrichttp.New(ctx)
	} else {
		metricExp, err = stdoutmetric.New()
	}
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		err1 := tp.Shutdown(ctx)
		if err2 := mp.Shutdown(ctx); err1 == nil {
			err1 = err2
		}
		return err1
	}, nil
}