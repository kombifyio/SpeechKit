//go:build windows

package main

import (
	"testing"
)

// TestTryAcquireSingletonFirstAndSecond exercises the belt-and-suspenders
// singleton mutex twice: the first acquisition must succeed, the second
// must report isFirst=false because the mutex is still held by the
// process under test. Releasing the first handle clears the OS state so
// the third acquisition can succeed again — important when other unit
// tests in the same process also touch this path.
func TestTryAcquireSingletonFirstAndSecond(t *testing.T) {
	release1, isFirst1, err1 := tryAcquireSingleton()
	if err1 != nil {
		t.Fatalf("first tryAcquireSingleton: %v", err1)
	}
	if !isFirst1 {
		t.Fatal("first tryAcquireSingleton: isFirst=false, want true on fresh acquisition")
	}
	if release1 == nil {
		t.Fatal("first tryAcquireSingleton: release=nil, want non-nil")
	}

	_, isFirst2, err2 := tryAcquireSingleton()
	if err2 != nil {
		t.Fatalf("second tryAcquireSingleton: %v", err2)
	}
	if isFirst2 {
		t.Fatal("second tryAcquireSingleton: isFirst=true, want false while first handle is held")
	}

	release1()

	release3, isFirst3, err3 := tryAcquireSingleton()
	if err3 != nil {
		t.Fatalf("third tryAcquireSingleton (after release): %v", err3)
	}
	if !isFirst3 {
		t.Fatal("third tryAcquireSingleton: isFirst=false, want true after first handle was released")
	}
	if release3 != nil {
		release3()
	}
}
