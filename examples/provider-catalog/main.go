// Example: reading SpeechKit's public mode and provider catalog.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func main() {
	jsonOutput := flag.Bool("json", false, "emit the public REST catalog JSON shape")
	modeInput := flag.String("mode", "", "optional mode filter (for example voice_agent)")
	checkOutput := flag.String("check-output", "", "compare JSON output byte-for-byte with this file")
	flag.Parse()

	if err := catalog.ValidateDefaultCatalog(); err != nil {
		log.Fatalf("invalid provider catalog: %v", err)
	}

	mode, err := parseMode(*modeInput)
	if err != nil {
		log.Fatal(err)
	}
	if *checkOutput != "" && !*jsonOutput {
		log.Fatal("--check-output requires --json")
	}

	if *jsonOutput {
		if err := emitJSON(os.Stdout, profilesForMode(mode)); err != nil {
			log.Fatalf("encode provider catalog: %v", err)
		}
		if *checkOutput != "" {
			if err := checkJSONOutput(*checkOutput, profilesForMode(mode)); err != nil {
				log.Fatal(err)
			}
			fmt.Fprintf(os.Stderr, "catalog output matches %s\n", *checkOutput)
		}
		return
	}

	for _, contract := range speechkit.DefaultModeContracts() {
		if mode != "" && contract.Mode != mode {
			continue
		}
		fmt.Printf("%s (%s): %s -> %s\n", contract.Mode, contract.Intelligence, contract.Input, contract.Output)
		for _, profile := range catalog.ProfilesForMode(contract.Mode) {
			fmt.Printf("  - %-28s %-18s %s\n", profile.ID, profile.ProviderKind, profile.Name)
		}
		fmt.Println()
	}
}

func parseMode(raw string) (speechkit.Mode, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	mode := speechkit.NormalizeMode(speechkit.Mode(raw))
	if mode != speechkit.ModeDictation && mode != speechkit.ModeAssist && mode != speechkit.ModeVoiceAgent && mode != speechkit.ModeTTS {
		return "", fmt.Errorf("unsupported mode %q", raw)
	}
	return mode, nil
}

func profilesForMode(mode speechkit.Mode) []speechkit.ProviderProfile {
	if mode == "" {
		return catalog.DefaultProviderProfiles()
	}
	return catalog.ProfilesForMode(mode)
}

func emitJSON(dst io.Writer, profiles []speechkit.ProviderProfile) error {
	return json.NewEncoder(dst).Encode(struct {
		Profiles []speechkit.ProviderProfile `json:"profiles"`
	}{Profiles: profiles})
}

func checkJSONOutput(path string, profiles []speechkit.ProviderProfile) error {
	want, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read --check-output %s: %w", path, err)
	}
	var got bytes.Buffer
	if err := emitJSON(&got, profiles); err != nil {
		return fmt.Errorf("encode catalog for comparison: %w", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		return fmt.Errorf("catalog output differs from %s", path)
	}
	return nil
}
