package runtimepath

import "testing"

// must returns a path function's result and fails the test on an error.
func must(t *testing.T, path func() (string, error)) string {
	t.Helper()
	got, err := path()
	if err != nil {
		t.Fatal(err)
	}
	return got
}
