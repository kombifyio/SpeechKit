package netsec

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kombifyio/SpeechKit/internal/dataprofilev1"
	"github.com/kombifyio/SpeechKit/internal/editionv1"
)

// BootFacts is the immutable process profile, suitable for configuration readback.
type BootFacts struct {
	Edition string              `json:"edition"`
	Profile dataprofile.Profile `json:"data_profile"`
}

type bootBinding struct {
	facts BootFacts
	err   error
}

var (
	bootOnce sync.Once
	boot     atomic.Pointer[bootBinding]
)

// BindBootProfile resolves the canonical edition/profile once, before credentials,
// clients or probes. A bad explicit edition is an error even though the canonical
// legacy Resolve helper defaults unknown editions to OSS. A failed bind stays
// failed for the process lifetime; changing the environment cannot loosen it.
func BindBootProfile() error {
	bootOnce.Do(func() {
		b := &bootBinding{}
		e, explicit := edition.ResolveStrict()
		if strings.TrimSpace(os.Getenv(edition.EnvVar)) != "" && !explicit {
			b.err = fmt.Errorf("%s: invalid %s", dataprofile.ReasonUnresolvable, edition.EnvVar)
		} else {
			b.facts.Edition = string(e)
			b.facts.Profile, b.err = dataprofile.FromBoot(e, os.Getenv(dataprofile.BootEnvVar))
		}
		if b.err == nil {
			b.err = b.facts.Profile.EnforceEgress()
		}
		boot.Store(b)
	})
	return boot.Load().err
}

// FullLocal reports the bound ceiling. It never rereads mutable environment or
// configuration. SDK users that do not bind a deployment profile retain their
// existing network policy.
func FullLocal() bool {
	b := boot.Load()
	return b != nil && b.err == nil && b.facts.Profile.Local()
}

// BoundBootFacts returns a value copy, never the binding itself.
func BoundBootFacts() (BootFacts, error) {
	if b := boot.Load(); b != nil {
		return b.facts, b.err
	}
	return BootFacts{}, fmt.Errorf("%s: boot profile is not bound", dataprofile.ReasonUnresolvable)
}

func bootError() error {
	if b := boot.Load(); b != nil {
		return b.err
	}
	return nil
}
