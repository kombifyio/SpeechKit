package audio

import "testing"

func TestListCaptureDevicesDelegatesToConfiguredLister(t *testing.T) {
	original := captureDeviceLister
	defer func() { captureDeviceLister = original }()

	var gotCfg Config
	captureDeviceLister = func(cfg Config) ([]DeviceInfo, error) {
		gotCfg = cfg
		return []DeviceInfo{{ID: "abc", Name: "Mic", IsDefault: true}}, nil
	}

	devices, err := ListCaptureDevices(Config{Backend: BackendAuto})
	if err != nil {
		t.Fatalf("ListCaptureDevices() error = %v", err)
	}
	if gotCfg.Backend != defaultBackend() {
		t.Fatalf("backend = %q, want %q", gotCfg.Backend, defaultBackend())
	}
	if len(devices) != 1 || devices[0].ID != "abc" || !devices[0].IsDefault {
		t.Fatalf("devices = %#v", devices)
	}
}

func TestListOutputDevicesDelegatesToConfiguredLister(t *testing.T) {
	original := outputDeviceLister
	defer func() { outputDeviceLister = original }()

	var gotCfg Config
	outputDeviceLister = func(cfg Config) ([]DeviceInfo, error) {
		gotCfg = cfg
		return []DeviceInfo{{ID: "speaker", Name: "Speakers", IsDefault: true}}, nil
	}

	devices, err := ListOutputDevices(Config{Backend: BackendAuto})
	if err != nil {
		t.Fatalf("ListOutputDevices() error = %v", err)
	}
	if gotCfg.Backend != defaultBackend() {
		t.Fatalf("backend = %q, want %q", gotCfg.Backend, defaultBackend())
	}
	if len(devices) != 1 || devices[0].ID != "speaker" || !devices[0].IsDefault {
		t.Fatalf("devices = %#v", devices)
	}
}

func TestSelectCaptureDeviceIDPrefersExactMatch(t *testing.T) {
	devices := []DeviceInfo{
		{ID: "111", Name: "Default", IsDefault: true},
		{ID: "abc123", Name: "External"},
	}

	if got := selectCaptureDeviceID("abc123", devices); got != "abc123" {
		t.Fatalf("selectCaptureDeviceID() = %q, want %q", got, "abc123")
	}
}

func TestSelectCaptureDeviceIDReturnsEmptyForUnknownID(t *testing.T) {
	devices := []DeviceInfo{
		{ID: "111", Name: "Default", IsDefault: true},
	}

	if got := selectCaptureDeviceID("does-not-exist", devices); got != "" {
		t.Fatalf("selectCaptureDeviceID() = %q, want empty", got)
	}
}

func TestSelectOutputDeviceIDMatchesConfiguredSpeaker(t *testing.T) {
	devices := []DeviceInfo{
		{ID: "default", Name: "System default", IsDefault: true},
		{ID: "speaker-2", Name: "Desk speakers"},
	}

	if got := selectOutputDeviceID("speaker-2", devices); got != "speaker-2" {
		t.Fatalf("selectOutputDeviceID() = %q, want speaker-2", got)
	}
}
