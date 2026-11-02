package telemetry

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"go.opentelemetry.io/otel"
)

// WithHTTPClient replaces the SDK's TLS and timeout configuration. Carry the
// same general-then-trace environment precedence on the guarded transport;
// partial trace certificate pairs retain the complete general pair.
func traceClientOptions() netsec.ClientOptions {
	options := netsec.ClientOptions{Timeout: 10 * time.Second}
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	for _, prefix := range []string{"OTEL_EXPORTER_OTLP_", "OTEL_EXPORTER_OTLP_TRACES_"} {
		if name := strings.TrimSpace(os.Getenv(prefix + "CERTIFICATE")); name != "" {
			data, err := os.ReadFile(name)
			if err != nil {
				otel.Handle(fmt.Errorf("read %sCERTIFICATE: %w", prefix, err))
			} else {
				pool := x509.NewCertPool()
				if pool.AppendCertsFromPEM(data) {
					config.RootCAs = pool
				} else {
					otel.Handle(fmt.Errorf("%sCERTIFICATE contains no CA certificate", prefix))
				}
			}
		}
		cert := strings.TrimSpace(os.Getenv(prefix + "CLIENT_CERTIFICATE"))
		key := strings.TrimSpace(os.Getenv(prefix + "CLIENT_KEY"))
		if cert != "" && key != "" {
			pair, err := tls.LoadX509KeyPair(cert, key)
			if err != nil {
				otel.Handle(fmt.Errorf("load %sCLIENT_CERTIFICATE/CLIENT_KEY: %w", prefix, err))
			} else {
				config.Certificates = []tls.Certificate{pair}
			}
		}
		if value := strings.TrimSpace(os.Getenv(prefix + "TIMEOUT")); value != "" {
			// OTLP timeout variables are integer milliseconds. Invalid values
			// leave the previous/default timeout, as the SDK's env reader does.
			if ms, err := strconv.ParseInt(value, 10, 64); err == nil && ms > 0 && ms <= math.MaxInt64/int64(time.Millisecond) {
				options.Timeout = time.Duration(ms) * time.Millisecond
			}
		}
	}
	options.TLSConfig = config
	return options
}
