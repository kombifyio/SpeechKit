//go:build windows

package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// hklmPoliciesParent is watched while the product's policy key does not
// exist yet, so a GPO that creates it is noticed.
const hklmPoliciesParent = `SOFTWARE\Policies`

// registryRoots are the three policy roots of a product, highest priority
// first: HKLM\SOFTWARE\Policies\<Vendor>\<Product>,
// HKLM\SOFTWARE\<Vendor>\<Product> and HKCU\Software\<Vendor>\<Product>.
type registryRoots struct {
	machinePolicies, machineDefaults, user string
}

func rootsFor(id PolicyIdentity) registryRoots {
	suffix := id.Vendor + `\` + id.Product
	return registryRoots{
		machinePolicies: `SOFTWARE\Policies\` + suffix,
		machineDefaults: `SOFTWARE\` + suffix,
		user:            `Software\` + suffix,
	}
}

// ReadPolicyValues reads the three registry roots of the registered
// PolicyIdentity and returns the effective policy overlay. The precedence and
// the HKCU trust rules live in the platform-neutral mergePolicyHives
// (policy.go); this file only reads. Missing keys and missing values mean "no
// override"; a value of the wrong type is reported in PolicyValues.Rejected.
// Without a registered identity nothing is read.
func ReadPolicyValues() PolicyValues {
	id, ok := RegisteredPolicyIdentity()
	if !ok {
		return noIdentityPolicy()
	}
	return readRegistryPolicy(rootsFor(id))
}

func readRegistryPolicy(roots registryRoots) PolicyValues {
	return mergePolicyHives(
		readPolicyHive(registrySource{hive: registry.LOCAL_MACHINE, root: roots.machinePolicies}),
		readPolicyHive(registrySource{hive: registry.LOCAL_MACHINE, root: roots.machineDefaults}),
		readPolicyHive(registrySource{hive: registry.CURRENT_USER, root: roots.user}),
	)
}

// registrySource reads policy values below one registry root. A key name
// `Section\Value` is the value Value of the subkey root\Section.
type registrySource struct {
	hive registry.Key
	root string
}

// open returns the subkey holding key and the value name, or ok=false when
// the subkey does not exist.
func (s registrySource) open(key string) (registry.Key, string, bool, error) {
	section, value, found := strings.Cut(key, `\`)
	if !found {
		return 0, "", false, fmt.Errorf("malformed policy key %q", key)
	}
	k, err := registry.OpenKey(s.hive, s.root+`\`+section, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return k, value, true, nil
}

func registryReadError(err error) error {
	if errors.Is(err, registry.ErrUnexpectedType) {
		return errPolicyType
	}
	return err
}

func (s registrySource) integer(key string) (*uint64, error) {
	k, name, ok, err := s.open(key)
	if !ok || err != nil {
		return nil, err
	}
	defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
	v, _, err := k.GetIntegerValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, registryReadError(err)
	}
	return &v, nil
}

func (s registrySource) boolValue(key string) (*bool, error) {
	v, err := s.integer(key)
	if v == nil || err != nil {
		return nil, err
	}
	b := *v != 0
	return &b, nil
}

func (s registrySource) intValue(key string) (*int, error) {
	v, err := s.integer(key)
	if v == nil || err != nil {
		return nil, err
	}
	if *v > uint64(int(^uint(0)>>1)) {
		return nil, errors.New("value out of range")
	}
	i := int(*v) // #nosec G115 -- guarded against int overflow before conversion.
	return &i, nil
}

func (s registrySource) stringValue(key string) (string, error) {
	k, name, ok, err := s.open(key)
	if !ok || err != nil {
		return "", err
	}
	defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
	v, _, err := k.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", registryReadError(err)
	}
	return strings.TrimSpace(v), nil
}

// listValue accepts REG_MULTI_SZ (one entry per string) and REG_SZ (entries
// separated by semicolons).
func (s registrySource) listValue(key string) ([]string, error) {
	k, name, ok, err := s.open(key)
	if !ok || err != nil {
		return nil, err
	}
	defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
	multi, _, err := k.GetStringsValue(name)
	switch {
	case err == nil:
		return splitPolicyList(strings.Join(multi, ";")), nil
	case errors.Is(err, registry.ErrNotExist):
		return nil, nil
	case !errors.Is(err, registry.ErrUnexpectedType):
		return nil, err
	}
	single, _, err := k.GetStringValue(name)
	if err != nil {
		return nil, registryReadError(err)
	}
	return splitPolicyList(single), nil
}

// watchPolicyChanges calls notify after every change below the product's
// machine Policies key (RegNotifyChangeKeyValue) and at least every interval, until
// ctx ends. Changes to HKLM\SOFTWARE\<Vendor>\<Product> and HKCU are picked up
// by the interval.
func watchPolicyChanges(ctx context.Context, interval time.Duration, notify func()) {
	id, ok := RegisteredPolicyIdentity()
	if !ok {
		return // no identity, no policy source to watch
	}
	policiesRoot := rootsFor(id).machinePolicies
	changed, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		slog.Warn("config: policy change notification unavailable; polling only", "err", err, "interval", interval)
		pollPolicyChanges(ctx, interval, notify)
		return
	}
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(changed)
		slog.Warn("config: policy change notification unavailable; polling only", "err", err, "interval", interval)
		pollPolicyChanges(ctx, interval, notify)
		return
	}
	go func() {
		<-ctx.Done()
		_ = windows.SetEvent(stop)
	}()
	go func() {
		// The registration is thread agnostic, but keep the thread anyway so
		// an older Windows build that ignores the flag still delivers it.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer windows.CloseHandle(changed) //nolint:errcheck // closing an event handle on shutdown is not actionable
		defer windows.CloseHandle(stop)    //nolint:errcheck // closing an event handle on shutdown is not actionable
		timeout := uint32(interval / time.Millisecond)
		// A registration fires once. It is re-armed on a fresh handle after
		// each change (the product key may have been created or deleted);
		// after a timeout the pending registration stays in place.
		key, armed := armPolicyNotification(policiesRoot, changed)
		defer func() {
			if armed {
				_ = key.Close()
			}
		}()
		for {
			index, err := windows.WaitForMultipleObjects([]windows.Handle{stop, changed}, false, timeout)
			if err != nil {
				slog.Warn("config: waiting for a policy change failed; polling only", "err", err)
				pollPolicyChanges(ctx, interval, notify)
				return
			}
			switch index {
			case windows.WAIT_OBJECT_0:
				return
			case windows.WAIT_OBJECT_0 + 1:
				// Group Policy writes a batch of values; let it finish.
				time.Sleep(time.Second)
				if armed {
					_ = key.Close()
				}
				// Closing the watched key signals the event once more.
				_ = windows.ResetEvent(changed)
				key, armed = armPolicyNotification(policiesRoot, changed)
			default:
				if !armed {
					key, armed = armPolicyNotification(policiesRoot, changed)
				}
			}
			notify()
		}
	}()
}

// armPolicyNotification registers changed for the next change below the
// product's policy key, or below HKLM\SOFTWARE\Policies while that key does
// not exist. ok=false means only the interval applies this round.
func armPolicyNotification(policiesRoot string, changed windows.Handle) (registry.Key, bool) {
	for _, path := range []string{policiesRoot, hklmPoliciesParent} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.NOTIFY)
		if err != nil {
			continue
		}
		filter := uint32(windows.REG_NOTIFY_CHANGE_NAME | windows.REG_NOTIFY_CHANGE_LAST_SET | windows.REG_NOTIFY_THREAD_AGNOSTIC)
		if err := windows.RegNotifyChangeKeyValue(windows.Handle(key), true, filter, changed, true); err != nil {
			_ = key.Close()
			slog.Debug("config: policy change notification not armed", "key", path, "err", err)
			return 0, false
		}
		return key, true
	}
	return 0, false
}
