package config

import "testing"

// The user hive is writable by the user, so it may only make updates more
// conservative: notify, never install, and never over a machine value.
func TestUpdateModeUserHiveMayOnlyChooseNotify(t *testing.T) {
	cases := []struct {
		name          string
		machine, user string
		want          string
	}{
		{"user asks for install, nothing pinned", "", UpdateModeInstall, UpdateModeInstall},
		{"user asks for notify", "", UpdateModeNotify, UpdateModeNotify},
		{"user cannot lift a machine notify", UpdateModeNotify, UpdateModeInstall, UpdateModeNotify},
		{"machine install stays install", UpdateModeInstall, UpdateModeNotify, UpdateModeInstall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			AttachPolicy(cfg, mergePolicyHives(PolicyValues{UpdateMode: tc.machine}, PolicyValues{}, PolicyValues{UpdateMode: tc.user}))
			if cfg.Update.Mode != tc.want {
				t.Fatalf("[update] mode = %q, want %q", cfg.Update.Mode, tc.want)
			}
		})
	}
}
