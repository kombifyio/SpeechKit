package procguard

import "testing"

func TestBundleHelpersDirMapsOnlyBundledExecutables(t *testing.T) {
	cases := map[string]string{
		"":                         "",
		"/usr/local/bin/speechkit": "",
		"/tmp/MacOS/SpeechKit":     "",
		"/Applications/SpeechKit.app/Contents/Resources/x":     "",
		"/Applications/SpeechKit.app/Contents/MacOS/SpeechKit": "/Applications/SpeechKit.app/Contents/Helpers",
	}
	for exe, want := range cases {
		if got := BundleHelpersDir(exe); got != want {
			t.Errorf("BundleHelpersDir(%q) = %q, want %q", exe, got, want)
		}
	}
}
