package auditlog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

var (
	otlpMu       sync.Mutex
	otlpProvider *sdklog.LoggerProvider
	// otlpAck shares otlpProvider's exporter but exports each record
	// synchronously, so a forward-required write learns whether the receiver
	// took it.
	otlpAck *sdklog.LoggerProvider
)

// otlpLoggerName is the instrumentation scope name every forwarded record
// carries to the receiver: <Vendor>.<Product>.AuditLog of the registered
// product identity, spaces as '-' ("kombify.SpeechKit.AuditLog" for the
// reference identity). Without a registered identity there is no name and
// the record is not exported: productid.ErrNoIdentity.
func otlpLoggerName() (string, error) {
	p, err := productid.Current()
	if err != nil {
		return "", err
	}
	scope := func(s string) string { return strings.ReplaceAll(s, " ", "-") }
	return scope(p.Vendor) + "." + scope(p.Product) + ".AuditLog", nil
}

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
	shutdownOTLPLocked(context.Background())

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
			caData, err := os.ReadFile(caFile) // #nosec G304 -- caFile is an operator-controlled OTLP TLS config path.
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
	otlpAck = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(ackProcessor{exp: exp}),
	)
	return nil
}

func shutdownOTLPLocked(ctx context.Context) {
	if otlpAck != nil {
		_ = otlpAck.Shutdown(ctx)
		otlpAck = nil
	}
	if otlpProvider != nil {
		_ = otlpProvider.Shutdown(ctx)
		otlpProvider = nil
	}
}

// ShutdownOTLP flushes pending records and closes the provider. Called from
// ResetForTests and during graceful shutdown. Safe to call when not configured.
func ShutdownOTLP() {
	otlpMu.Lock()
	defer otlpMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownOTLPLocked(ctx)
}

// emitToOTLP exports one audit Record as an OTLP log record, best effort
// through the batch processor: failures are dropped (the file sink has the
// record). Used when forward_required is off.
func emitToOTLP(ctx context.Context, rec Record) {
	otlpMu.Lock()
	p := otlpProvider
	otlpMu.Unlock()
	if p == nil {
		return
	}
	name, err := otlpLoggerName()
	if err != nil {
		return
	}
	p.Logger(name).Emit(context.WithoutCancel(ctx), otlpRecord(rec))
}

func otlpRecord(rec Record) otellog.Record {
	var r otellog.Record
	r.SetTimestamp(rec.Timestamp)
	r.SetObservedTimestamp(time.Now().UTC())
	r.SetBody(attribute.StringValue(string(rec.Event)))

	// Map audit outcome to OTel severity.
	if rec.Outcome == OutcomeFailure {
		r.SetSeverity(otellog.SeverityWarn)
		r.SetSeverityText("WARN")
	} else {
		r.SetSeverity(otellog.SeverityInfo)
		r.SetSeverityText("INFO")
	}

	r.AddAttributes(
		attribute.String("_schema_version", rec.SchemaVersion),
		attribute.Int64("seq", int64(rec.Seq)), // #nosec G115 -- a per-install counter never reaches 2^63
		attribute.String("event", string(rec.Event)),
		attribute.String("outcome", string(rec.Outcome)),
		attribute.String("actor.user_sid", rec.Actor.UserSID),
		attribute.String("actor.session_id", rec.Actor.SessionID),
		attribute.String("trace_id", rec.TraceID),
		attribute.String("hash", rec.Hash),
	)
	// Flatten the resource map as individual attributes with a "resource." prefix.
	for k, v := range rec.Resource {
		r.AddAttributes(attribute.KeyValue{Key: attribute.Key("resource." + k), Value: anyToOTELValue(v)})
	}
	return r
}

// ackKey carries an *ackResult through Logger.Emit to ackProcessor.
type ackKey struct{}

type ackResult struct {
	exported bool
	err      error
}

// ackProcessor exports every record synchronously and reports the outcome
// through the context, because Logger.Emit itself returns nothing.
type ackProcessor struct{ exp sdklog.Exporter }

func (ackProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (p ackProcessor) OnEmit(ctx context.Context, r *sdklog.Record) error {
	err := p.exp.Export(ctx, []sdklog.Record{r.Clone()})
	if res, ok := ctx.Value(ackKey{}).(*ackResult); ok {
		res.exported = true
		res.err = err
	}
	return err
}

// Shutdown and ForceFlush are no-ops: the exporter belongs to the batch
// provider, which flushes and shuts it down.
func (ackProcessor) Shutdown(context.Context) error   { return nil }
func (ackProcessor) ForceFlush(context.Context) error { return nil }

// otlpForwarder is OTLP as a forward-required sink: Forward returns nil only
// when the receiver accepted the export.
type otlpForwarder struct{}

func (otlpForwarder) Name() string { return "otlp" }

func (otlpForwarder) Bound() time.Duration { return OTLPForwardBound }

func (otlpForwarder) Forward(ctx context.Context, rec Record) error {
	otlpMu.Lock()
	p := otlpAck
	otlpMu.Unlock()
	if p == nil {
		return errNoForwardSink
	}
	name, err := otlpLoggerName()
	if err != nil {
		return err
	}
	res := &ackResult{}
	p.Logger(name).Emit(context.WithValue(ctx, ackKey{}, res), otlpRecord(rec))
	if !res.exported {
		return errNotExported
	}
	return res.err
}

// otlpSink returns the configured OTLP sink, or nil.
func otlpSink() Forwarder {
	otlpMu.Lock()
	defer otlpMu.Unlock()
	if otlpAck == nil {
		return nil
	}
	return otlpForwarder{}
}

// anyToOTELValue converts a Go any value from Record.Resource to an
// attribute.Value. Covers the concrete types used in SpeechKit audit records;
// everything else falls back to a string representation.
func anyToOTELValue(v any) attribute.Value {
	switch x := v.(type) {
	case string:
		return attribute.StringValue(x)
	case bool:
		return attribute.BoolValue(x)
	case int:
		return attribute.IntValue(x)
	case int64:
		return attribute.Int64Value(x)
	case float64:
		return attribute.Float64Value(x)
	default:
		return attribute.StringValue(fmt.Sprintf("%v", v))
	}
}
