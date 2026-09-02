package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

// ExampleNew talks to a SpeechKit server over its public HTTP contract. The
// httptest server below stands in for a real `speechkit-server`; in production
// point BaseURL at your deployment and pass the bearer token the server was
// configured with. client.FromEnv reads SPEECHKIT_SERVER_URL and
// SPEECHKIT_TOKEN for the same purpose.
func ExampleNew() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/readyz":
			_ = json.NewEncoder(w).Encode(client.Status{Status: "ok", Version: "example"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, err := client.New(client.Options{BaseURL: server.URL, Token: "secret"})
	if err != nil {
		log.Fatal(err)
	}

	status, err := c.Status(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(status.Status, status.Version)
	// Output: ok example
}

// ExampleHTTPError shows that server-side failures surface as client.HTTPError
// with the status code, so hosts can distinguish auth problems from outages
// without parsing message text.
func ExampleHTTPError() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"token rejected"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	c, _ := client.New(client.Options{BaseURL: server.URL, Token: "wrong"})
	_, err := c.Status(context.Background())

	var httpErr client.HTTPError
	if errors.As(err, &httpErr) {
		fmt.Println(httpErr.StatusCode)
	}
	// Output: 401
}
