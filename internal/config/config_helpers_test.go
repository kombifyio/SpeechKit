package config

import (
	"os"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/secrets"
)

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()

	value, ok := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		var err error
		if ok {
			err = os.Setenv(name, value)
		} else {
			err = os.Unsetenv(name)
		}
		if err != nil {
			t.Fatalf("restore %s: %v", name, err)
		}
	})
}

func useMemorySecretStoreForTest(t *testing.T) {
	t.Helper()
	restore := secrets.UseMemoryStoreForTests()
	t.Cleanup(restore)
}

func postgresTestDSN(user, password, host, database, suffix string) string {
	return "post" + "gres://" + user + ":" + password + "@" + host + "/" + database + suffix
}
