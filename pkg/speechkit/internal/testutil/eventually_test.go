package testutil

import (
	"errors"
	"testing"
	"time"
)

func TestEventuallyReturnsWhenConditionPasses(t *testing.T) {
	attempts := 0
	Eventually(t, time.Second, time.Millisecond, func() bool {
		attempts++
		return attempts >= 2
	})
}

func TestEventuallyNoErrorReturnsWhenFunctionSucceeds(t *testing.T) {
	attempts := 0
	EventuallyNoError(t, time.Second, time.Millisecond, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("not yet")
		}
		return nil
	})
}
