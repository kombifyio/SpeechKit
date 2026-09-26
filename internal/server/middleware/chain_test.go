//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChain_OrderOutermostFirst(t *testing.T) {
	var order []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+":before")
				next.ServeHTTP(w, r)
				order = append(order, name+":after")
			})
		}
	}
	handler := Chain(mw("a"), mw("b"), mw("c"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"a:before", "b:before", "c:before", "handler", "c:after", "b:after", "a:after"}
	if len(order) != len(want) {
		t.Fatalf("unexpected length: got %v want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q want %q", i, order[i], want[i])
		}
	}
}
