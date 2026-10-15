package config_test

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/config"
)

func sharedTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Store.SaveAudio = true
	cfg.UI.OverlayMonitorPositions = map[string]config.OverlayFreePosition{"a": {X: 1, Y: 1}}
	return cfg
}

func TestSharedFailedUpdateLeavesTheConfigUnchanged(t *testing.T) {
	s := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), sharedTestConfig())

	_, err := s.Update(func(c *config.Config) error {
		c.Store.SaveAudio = false
		c.UI.OverlayMonitorPositions["a"] = config.OverlayFreePosition{X: 9, Y: 9}
		return errors.New("refused")
	})

	if err == nil {
		t.Fatal("the failing edit reported success")
	}
	got := s.Snapshot()
	if !got.Store.SaveAudio || got.UI.OverlayMonitorPositions["a"].X != 1 {
		t.Fatalf("a failed edit changed the config: save_audio=%v position=%v", got.Store.SaveAudio, got.UI.OverlayMonitorPositions["a"])
	}
}

func TestSharedUpdateRefusesAPolicyPinnedValue(t *testing.T) {
	cfg := sharedTestConfig()
	config.AttachPolicy(cfg, config.PolicyValues{AudioSaveAudio: new(false)})
	s := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), cfg)

	_, err := s.UpdateAndSave(func(c *config.Config) error {
		c.Store.SaveAudio = true
		return nil
	})

	var locked *config.PolicyLockedError
	if !errors.As(err, &locked) {
		t.Fatalf("want a *PolicyLockedError, got %v", err)
	}
	if s.Snapshot().Store.SaveAudio {
		t.Fatal("the pinned value was written")
	}
}

func TestSharedSubscribersSeeOldAndNew(t *testing.T) {
	s := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), sharedTestConfig())
	var seen []config.Change
	cancel := s.Subscribe(func(ch config.Change) { seen = append(seen, ch) })

	if _, err := s.Update(func(c *config.Config) error { c.Store.SaveAudio = false; return nil }); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := s.Update(func(c *config.Config) error { c.Store.SaveAudio = true; return nil }); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 1 {
		t.Fatalf("subscriber ran %d times; it should run once before cancel and never after", len(seen))
	}
	if ch := seen[0]; !ch.Old.Store.SaveAudio || ch.New.Store.SaveAudio || ch.Cause != config.CauseUserEdit {
		t.Fatalf("change: old=%v new=%v cause=%v", ch.Old.Store.SaveAudio, ch.New.Store.SaveAudio, ch.Cause)
	}
}

// A snapshot is a deep copy: editing it, maps included, never reaches the
// shared config.
func TestSharedSnapshotIsIndependent(t *testing.T) {
	s := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), sharedTestConfig())

	snap := s.Snapshot()
	snap.UI.OverlayMonitorPositions["a"] = config.OverlayFreePosition{X: 5, Y: 5}
	snap.Store.SaveAudio = false

	if got := s.Snapshot(); got.UI.OverlayMonitorPositions["a"].X != 1 || !got.Store.SaveAudio {
		t.Fatal("an edit of a snapshot reached the shared config")
	}
}

// Run with -race: concurrent writers and readers never touch the same
// memory without the lock.
func TestSharedConcurrentUpdateAndSnapshot(t *testing.T) {
	s := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), sharedTestConfig())
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := range 50 {
				_, _ = s.Update(func(c *config.Config) error {
					c.UI.OverlayMonitorPositions["a"] = config.OverlayFreePosition{X: i, Y: j}
					return nil
				})
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				snap := s.Snapshot()
				for k, v := range snap.UI.OverlayMonitorPositions {
					_, _ = k, v
				}
			}
		}()
	}
	wg.Wait()
	if _, ok := s.Snapshot().UI.OverlayMonitorPositions["a"]; !ok {
		t.Fatal("the shared config lost its value under concurrent writes")
	}
}

// A policy refresh is the policy itself: it commits a tightening of a value
// the bound policy pins, and subscribers learn it came from a refresh.
func TestSharedPolicyRefreshCommitsATightening(t *testing.T) {
	cfg := sharedTestConfig()
	config.AttachPolicy(cfg, config.PolicyValues{AudioSaveAudio: new(true)})
	s := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), cfg)
	var cause config.Cause = -1
	s.Subscribe(func(ch config.Change) { cause = ch.Cause })

	refresh := s.RefreshPolicy(config.PolicyValues{AudioSaveAudio: new(false)})

	if len(refresh.Tightened) == 0 || s.Snapshot().Store.SaveAudio {
		t.Fatalf("the tightening was not committed: tightened=%v save_audio=%v", refresh.Tightened, s.Snapshot().Store.SaveAudio)
	}
	if cause != config.CausePolicyRefresh {
		t.Fatalf("subscriber saw cause %v, want %v", cause, config.CausePolicyRefresh)
	}
}

// An edit prepared on a snapshot commits only what it changed: a policy
// tightening committed while the caller prepared it stays.
func TestSharedUpdateFromKeepsAChangeCommittedMeanwhile(t *testing.T) {
	cfg := sharedTestConfig()
	config.AttachPolicy(cfg, config.PolicyValues{AudioSaveAudio: new(true)})
	s := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), cfg)
	base := s.Snapshot()
	edited := base.Clone()
	edited.UI.OverlayMonitorPositions["a"] = config.OverlayFreePosition{X: 7, Y: 7}

	s.RefreshPolicy(config.PolicyValues{AudioSaveAudio: new(false)})
	if _, err := s.UpdateFrom(base, edited); err != nil {
		t.Fatal(err)
	}

	got := s.Snapshot()
	if got.UI.OverlayMonitorPositions["a"].X != 7 || got.Store.SaveAudio {
		t.Fatalf("position=%v save_audio=%v: the edit must land and the tightening must stay", got.UI.OverlayMonitorPositions["a"], got.Store.SaveAudio)
	}
}
