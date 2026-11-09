package cloudlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth outcomes the link reacts to; anything else is a plain error.
var (
	errAuthorizationPending = errors.New("authorization pending")
	errSlowDown             = errors.New("slow down")
	// ErrExpired means the person did not approve before the code expired.
	ErrExpired = errors.New("the approval code expired before it was confirmed; start the link again")
	// ErrDenied means the person refused the request.
	ErrDenied = errors.New("the link request was refused on the approval page")
	// ErrInvalidGrant means the refresh token is spent or revoked; only a new
	// link produces one again.
	ErrInvalidGrant = errors.New("the stored credential is no longer valid; link this server again")
)

// providerMetadata is the subset of the issuer's OpenID configuration the
// device grant needs.
type providerMetadata struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	RevocationEndpoint          string `json:"revocation_endpoint"`
}

// DeviceAuthorization is a started link: what the person needs to approve.
type DeviceAuthorization struct {
	DeviceCode              string    `json:"-"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
	ExpiresAt               time.Time `json:"expires_at"`
	Interval                int       `json:"interval_seconds"`
}

type tokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type oauthClient struct {
	settings Settings
	http     *http.Client
	meta     *providerMetadata
}

func (c *oauthClient) metadata(ctx context.Context) (*providerMetadata, error) {
	if c.meta != nil {
		return c.meta, nil
	}
	endpoint := strings.TrimRight(c.settings.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read the issuer configuration: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return nil, fmt.Errorf("read the issuer configuration: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("issuer configuration %s answered HTTP %d; check [server.cloud_link] issuer", endpoint, resp.StatusCode)
	}
	var meta providerMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("issuer configuration: %w", err)
	}
	if meta.DeviceAuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return nil, fmt.Errorf("issuer %s offers no device authorization grant (device_authorization_endpoint missing)", c.settings.Issuer)
	}
	for _, e := range []string{meta.DeviceAuthorizationEndpoint, meta.TokenEndpoint} {
		if err := checkEndpointURL(e); err != nil {
			return nil, fmt.Errorf("issuer configuration: %w", err)
		}
	}
	if meta.RevocationEndpoint != "" && checkEndpointURL(meta.RevocationEndpoint) != nil {
		meta.RevocationEndpoint = ""
	}
	c.meta = &meta
	return c.meta, nil
}

func (c *oauthClient) startDeviceAuthorization(ctx context.Context) (*DeviceAuthorization, error) {
	meta, err := c.metadata(ctx)
	if err != nil {
		return nil, err
	}
	var body struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	form := url.Values{
		"client_id": {c.settings.ClientID},
		"scope":     {c.settings.Scope},
		"audience":  {c.settings.Audience},
	}
	if err := c.postForm(ctx, meta.DeviceAuthorizationEndpoint, form, &body); err != nil {
		return nil, err
	}
	if body.DeviceCode == "" || body.UserCode == "" || body.VerificationURI == "" {
		return nil, errors.New("the device authorization endpoint returned an incomplete answer")
	}
	if body.Interval <= 0 {
		body.Interval = 5
	}
	if body.ExpiresIn <= 0 {
		body.ExpiresIn = 600
	}
	if body.VerificationURIComplete == "" {
		body.VerificationURIComplete = body.VerificationURI
	}
	return &DeviceAuthorization{
		DeviceCode:              body.DeviceCode,
		UserCode:                body.UserCode,
		VerificationURI:         body.VerificationURI,
		VerificationURIComplete: body.VerificationURIComplete,
		ExpiresAt:               time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
		Interval:                body.Interval,
	}, nil
}

// awaitApproval polls the token endpoint at the advertised interval until
// the person approves, refuses, or the code expires.
func (c *oauthClient) awaitApproval(ctx context.Context, da *DeviceAuthorization) (*tokenSet, error) {
	meta, err := c.metadata(ctx)
	if err != nil {
		return nil, err
	}
	interval := time.Duration(da.Interval) * time.Second
	for {
		if time.Now().After(da.ExpiresAt) {
			return nil, ErrExpired
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		tokens, err := c.exchange(ctx, meta.TokenEndpoint, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {da.DeviceCode},
			"client_id":   {c.settings.ClientID},
		})
		switch {
		case errors.Is(err, errAuthorizationPending):
			continue
		case errors.Is(err, errSlowDown):
			interval += 5 * time.Second
			continue
		case err != nil:
			return nil, err
		}
		if tokens.RefreshToken == "" {
			return nil, errors.New("the identity provider issued no refresh token; the client needs the refresh token grant and offline_access")
		}
		return tokens, nil
	}
}

func (c *oauthClient) refresh(ctx context.Context, refreshToken string) (*tokenSet, error) {
	meta, err := c.metadata(ctx)
	if err != nil {
		return nil, err
	}
	return c.exchange(ctx, meta.TokenEndpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.settings.ClientID},
	})
}

// revoke revokes the refresh token (RFC 7009) when the issuer offers it.
// ok is false when the issuer names no revocation endpoint.
func (c *oauthClient) revoke(ctx context.Context, refreshToken string) (bool, error) {
	meta, err := c.metadata(ctx)
	if err != nil {
		return false, err
	}
	if meta.RevocationEndpoint == "" {
		return false, nil
	}
	return true, c.postForm(ctx, meta.RevocationEndpoint, url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {c.settings.ClientID},
	}, nil)
}

func (c *oauthClient) exchange(ctx context.Context, endpoint string, form url.Values) (*tokenSet, error) {
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := c.postForm(ctx, endpoint, form, &body); err != nil {
		return nil, err
	}
	if body.AccessToken == "" {
		return nil, errors.New("the token endpoint returned no access token")
	}
	if body.ExpiresIn <= 0 {
		body.ExpiresIn = 3600
	}
	return &tokenSet{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// postForm sends one form POST; out may be nil. The response body is never
// echoed into an error: a token endpoint can reflect credential material.
func (c *oauthClient) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("identity provider: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("identity provider: %w", err)
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(payload, &e)
		switch e.Error {
		case "authorization_pending":
			return errAuthorizationPending
		case "slow_down":
			return errSlowDown
		case "expired_token":
			return ErrExpired
		case "access_denied":
			return ErrDenied
		case "invalid_grant":
			return ErrInvalidGrant
		case "":
			return fmt.Errorf("identity provider answered HTTP %d", resp.StatusCode)
		}
		if e.Description != "" {
			return fmt.Errorf("identity provider: %s: %s", e.Error, e.Description)
		}
		return fmt.Errorf("identity provider: %s", e.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("identity provider: parse answer: %w", err)
	}
	return nil
}
