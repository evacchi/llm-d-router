/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestInitTracingWithDeploymentVersion(t *testing.T) {
	t.Setenv("DEPLOYMENT_VERSION", "canary-v2")
	t.Setenv("OTEL_TRACES_EXPORTER", "console")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "1.0")

	logger := zap.New(zap.UseDevMode(true))

	err := InitTracing(context.Background(), logger, "test-service")
	require.NoError(t, err)

	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	require.True(t, ok, "expected SDK TracerProvider")
	defer func() { require.NoError(t, tp.Shutdown(context.Background())) }()

	exporter := tracetest.NewInMemoryExporter()
	tp.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter))

	_, span := tp.Tracer("test").Start(context.Background(), "test-span")
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	found := false
	for _, kv := range spans[0].Resource.Attributes() {
		if kv.Key == attribute.Key("llm_d.deployment_version") {
			require.Equal(t, "canary-v2", kv.Value.AsString())
			found = true
			break
		}
	}
	require.True(t, found, "expected llm_d.deployment_version resource attribute")
}

func TestInitTracingWithoutDeploymentVersion(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "console")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "1.0")

	logger := zap.New(zap.UseDevMode(true))

	err := InitTracing(context.Background(), logger, "test-service")
	require.NoError(t, err)

	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	require.True(t, ok, "expected SDK TracerProvider")
	defer func() { require.NoError(t, tp.Shutdown(context.Background())) }()

	exporter := tracetest.NewInMemoryExporter()
	tp.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter))

	_, span := tp.Tracer("test").Start(context.Background(), "test-span")
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	for _, kv := range spans[0].Resource.Attributes() {
		require.NotEqual(t, attribute.Key("llm_d.deployment_version"), kv.Key,
			"llm_d.deployment_version should not be present when env var is unset")
	}
}
