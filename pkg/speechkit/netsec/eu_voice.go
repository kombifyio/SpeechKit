package netsec

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/dataprofilev1"
)

func DataProfileEU(ctx context.Context) bool {
	profile, ok := dataprofile.FromContext(ctx)
	return ok && profile.EU()
}

func EUVoiceProfileError(ctx context.Context) error {
	profile, ok := dataprofile.FromContext(ctx)
	if ok && profile.EU() && profile.Zero() {
		return fmt.Errorf("%s: EU voice with zero retention is not qualified", dataprofile.ReasonProcessorIneligible)
	}
	return nil
}

func EUVoiceClient(client *http.Client) *http.Client {
	copy := *client
	previous := client.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 || len(via) >= 10 || !strings.EqualFold(req.URL.Scheme, "https") || !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return fmt.Errorf("netsec: regional voice redirect rejected")
		}
		if previous != nil {
			if err := previous(req, via); err != nil {
				return err
			}
			if !strings.EqualFold(req.URL.Scheme, "https") || !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return fmt.Errorf("netsec: regional voice redirect rejected")
			}
		}
		return nil
	}
	return &copy
}
