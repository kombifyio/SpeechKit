package hostconfig_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"
)

func FuzzParseNormalize(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("[general]\nhotkey_behavior = \"push_to_talk\"\n"))
	f.Add([]byte("[server_connection]\nurl = \"https://example.test/\"\nauth_mode = \"api_key\"\n"))
	f.Add([]byte("[general]\n\xff\xfe = ["))

	path := filepath.Join(f.TempDir(), "config.toml")

	f.Fuzz(func(t *testing.T, data []byte) {
		if cfg, err := hostconfig.Parse(data); err == nil {
			hostconfig.Normalize(cfg, nil, hostconfig.LegacyGeneral{})
			once := *cfg
			hostconfig.Normalize(cfg, nil, hostconfig.LegacyGeneral{})
			if !reflect.DeepEqual(once, *cfg) {
				t.Fatalf("Normalize is not idempotent:\nonce  %+v\ntwice %+v", once, *cfg)
			}
		}

		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = hostconfig.LoadConfig(path)
	})
}
