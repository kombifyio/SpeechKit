package auditlog

import (
	"context"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

// scopeExporter records the instrumentation scope of every exported record.
type scopeExporter struct{ scopes []string }

func (e *scopeExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, r := range records {
		e.scopes = append(e.scopes, r.InstrumentationScope().Name)
	}
	return nil
}
func (*scopeExporter) Shutdown(context.Context) error   { return nil }
func (*scopeExporter) ForceFlush(context.Context) error { return nil }

// The scope name reaches the customer's receiver with every record, so it
// names the product the record came from: the reference keeps its name, a
// managed product carries its own and none of the reference's.
func TestForwardedRecordCarriesTheProductScopeName(t *testing.T) {
	example := productid.Product{
		Vendor: "Contoso", Product: "Example Dictate", BundleID: "com.contoso.dictate",
		AppName: "Dictate", DataDirName: "Example Dictate", SingleInstanceID: "com.contoso.dictate",
		CachePrefix: "contoso-dictate", RecordPrefix: "EXAMPLE_DICTATE", ExecutableName: "ExampleDictate.exe",
	}
	for _, tc := range []struct {
		identity productid.Product
		want     string
	}{
		{productid.Reference, "kombify.SpeechKit.AuditLog"},
		{example, "Contoso.Example-Dictate.AuditLog"},
	} {
		productid.UseForTest(t, tc.identity)
		exp := &scopeExporter{}
		otlpMu.Lock()
		otlpAck = sdklog.NewLoggerProvider(sdklog.WithProcessor(ackProcessor{exp: exp}))
		otlpMu.Unlock()
		if err := (otlpForwarder{}).Forward(context.Background(), Record{Event: EventProviderSelected}); err != nil {
			t.Fatalf("%s: Forward: %v", tc.want, err)
		}
		if len(exp.scopes) != 1 || exp.scopes[0] != tc.want {
			t.Errorf("forwarded scope names %v, want %q", exp.scopes, tc.want)
		}
	}
	otlpMu.Lock()
	otlpAck = nil
	otlpMu.Unlock()
}
