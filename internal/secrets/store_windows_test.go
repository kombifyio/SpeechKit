//go:build windows

package secrets

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestWindowsDPAPIHelpersAcceptEmptyPayload(t *testing.T) {
	if protected, err := protectWithDPAPI(nil); err != nil || protected != nil {
		t.Fatalf("protect empty = (%v, %v), want nil nil", protected, err)
	}
	if plain, err := unprotectWithDPAPI(nil); err != nil || plain != nil {
		t.Fatalf("unprotect empty = (%v, %v), want nil nil", plain, err)
	}
}

func TestWindowsDPAPIHelpersRoundTrip(t *testing.T) {
	protected, err := protectWithDPAPI([]byte("secret-value"))
	if err != nil {
		t.Fatalf("protect: %v", err)
	}
	if bytes.Equal(protected, []byte("secret-value")) {
		t.Fatal("protected payload should not equal plaintext")
	}
	plain, err := unprotectWithDPAPI(protected)
	if err != nil {
		t.Fatalf("unprotect: %v", err)
	}
	assertTestSecret(t, "decrypted secret", string(plain), "secret-value")
}

func TestWindowsConfigureDopplerCommandHidesWindow(t *testing.T) {
	cmd := exec.Command("doppler")
	configureDopplerCommand(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v, want HideWindow", cmd.SysProcAttr)
	}
}
