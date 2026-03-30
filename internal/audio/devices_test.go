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
