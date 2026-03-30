//go:build !windows

package secrets

func MigrateInstallTokenBootstrap() (bool, error) {
	return false, nil
}
