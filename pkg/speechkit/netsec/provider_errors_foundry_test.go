package netsec

import (
	"net/http"
	"strings"
	"testing"
)

// A Foundry 404 used to read as "provider rejected request", which sent the
// user looking for an outage instead of at the deployment name.
func TestSafeProviderErrorReasonNamesMissingDeployments(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"azure DeploymentNotFound", http.StatusNotFound, `{"error":{"code":"DeploymentNotFound","message":"The API deployment for this resource does not exist."}}`, "deployment not found"},
		{"spaced wording", http.StatusNotFound, `{"error":"deployment not found"}`, "deployment not found"},
		{"plain 404", http.StatusNotFound, `not here`, "provider endpoint or model not found"},
		{"other 4xx stays generic", http.StatusUnprocessableEntity, `{"error":"bad field"}`, "provider rejected request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeProviderErrorReason(tc.status, []byte(tc.body))
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("reason = %q, want prefix %q", got, tc.want)
			}
			if strings.Contains(got, "The API deployment") {
				t.Fatalf("response body leaked into %q", got)
			}
		})
	}
}
