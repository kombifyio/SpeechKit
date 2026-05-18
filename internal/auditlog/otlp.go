package auditlog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

var (
	otlpMu       sync.Mutex
	otlpProvider *sdklog.LoggerProvider
)

// ConfigureOTLP creates an OTLP exporter targeting endpoint with optional
// mTLS. Returns nil when endpoint is empty (sink disabled). Idempotent:
// re-calling shuts down the previous provider and starts a new one.
//
// Endpoint format: hostname:port (e.g. "loki.internal.example:4318").
// HTTPS with mTLS is used when certFile + keyFile are provided.
// Plain HTTP (no TLS) is used when no cert files are present — suitable
// for dev/test deployments behind an authenticating proxy.
//
// caFile is optional; when non-empty its PEM certificate(s) are added to
// the root CA pool for server-certificate verification.
func ConfigureOTLP(endpoint, certFile, keyFile, caFile string) error {
	otlpMu.Lock()
	defer otlpMu.Unlock()

	// Tear down previous provider if any (supports re-config in tests).
	if otlpProvider != nil {
		_ = otlpProvider.Shutdown(context.Background())
		otlpProvider = nil
	}

	if endpoint == "" {
		return nil
	}

	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(endpoint),
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("load mtls client cert: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		if caFile != "" {
			caData, err := os.ReadFile(caFile)
			if err != nil {
				return fmt.Errorf("read ca file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caData) {
				return fmt.Errorf("ca file %q contained no parseable PEM certs", caFile)
			}
			tlsCfg.RootCAs = pool
		}
		opts = append(opts, otlploghttp.WithTLSClientConfig(tlsCfg))
	} else {
		// Plain HTTP — no TLS. Suitable for dev deployments and environments
		// where TLS termination is handled by a sidecar or proxy.
		opts = append(opts, otlploghttp.WithInsecure())
	}

	exp, err := otlploghttp.New(context.Background(), opts...)
	if err != nil {
		return fmt.Errorf("create otlp exporter: %w", err)
	}

	otlpProvider = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
	)
	return nil
}

// ShutdownOTLP flushes pending records and closes the provider. Called from
// ResetForTests and during graceful shutdown. Safe to call when not configured.
func ShutdownOTLP() {
	otlpMu.Lock()
	defer otlpMu.Unlock()
	if otlpProvider == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = otlpProvider.Shutdown(ctx)
	otlpProvider = nil
}

// emitToOTLP exports one audit Record as an OTLP log record.
// Called from AppendEvent after the file sink write. Failures are silently
// dropped — the file sink is canonical; OTLP is a supplementary sink.
// Uses its own otlpMu — no deadlock risk with the outer mu in auditlog.go.
func emitToOTLP(rec Record) {
	otlpMu.Lock()
	p := otlpProvider
	otlpMu.Unlock()
	if p == nil {
		return
	}

	logger := p.Logger("kombify.SpeechKit.AuditLog")

	var r otellog.Record
	r.SetTimestamp(rec.Timestamp)
	r.SetObservedTimestamp(time.Now().UTC())
	r.SetBody(otellog.StringValue(string(rec.Event)))

	// Map audit outcome to OTel severity.
	if rec.Outcome == OutcomeFailure {
		r.SetSeverity(otellog.SeverityWarn)
		r.SetSeverityText("WARN")
	} else {
		r.SetSeverity(otellog.SeverityInfo)
		r.SetSeverityText("INFO")
	}

	r.AddAttributes(
		otellog.String("_schema_version", rec.SchemaVersion),
		otellog.String("event", string(rec.Event)),
		otellog.String("outcome", string(rec.Outcome)),
		otellog.String("actor.user_sid", rec.Actor.UserSID),
		otellog.String("actor.session_id", rec.Actor.SessionID),
		otellog.String("trace_id", rec.TraceID),
	)
	// Flatten the resource map as individual attributes with a "resource." prefix.
	for k, v := range rec.Resource {
		r.AddAttributes(otellog.KeyValue{Key: "resource." + k, Value: anyToOTELValue(v)})
	}

	logger.Emit(context.Background(), r)
}

// anyToOTELValue converts a Go any value from Record.Resource to an
// otellog.Value. Covers the concrete types used in SpeechKit audit records;
// everything else falls back to a string representation.
func anyToOTELValue(v any) otellog.Value {
	switch x := v.(type) {
	case string:
		return otellog.StringValue(x)
	case bool:
		return otellog.BoolValue(x)
	case int:
		return otellog.IntValue(x)
	case int64:
		return otellog.Int64Value(x)
	case float64:
		return otellog.Float64Value(x)
	default:
		return otellog.StringValue(fmt.Sprintf("%v", v))
	}
}
