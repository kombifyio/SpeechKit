//go:build windows || (darwin && cgo)

package secrets

import (
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

// A root that registered no product identity has no secrets directory to
// read or write: the store fails closed instead of using a shared or relative
// location.
func TestSecretStoreWithoutProductIdentityFailsClosed(t *testing.T) {
	redirectSecretsDir(t)
	productid.ClearForTest(t)
	if err := SetNamedSecret("probe", "value"); !errors.Is(err, productid.ErrNoIdentity) {
		t.Fatalf("store without identity: want ErrNoIdentity, got %v", err)
	}
	if _, _, err := ResolveNamedSecret("probe", nil); !errors.Is(err, productid.ErrNoIdentity) {
		t.Fatalf("resolve without identity: want ErrNoIdentity, got %v", err)
	}
}
