// Example: reading SpeechKit's public mode and provider catalog.
package main

import (
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func main() {
	if err := catalog.ValidateDefaultCatalog(); err != nil {
		log.Fatalf("invalid provider catalog: %v", err)
	}

	for _, contract := range speechkit.DefaultModeContracts() {
		fmt.Printf("%s (%s): %s -> %s\n", contract.Mode, contract.Intelligence, contract.Input, contract.Output)
		for _, profile := range catalog.ProfilesForMode(contract.Mode) {
			fmt.Printf("  - %-28s %-18s %s\n", profile.ID, profile.ProviderKind, profile.Name)
		}
		fmt.Println()
	}
}
