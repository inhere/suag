package discount

import "testing"

func TestValidate(t *testing.T) {
	if err := Validate(30); err != nil {
		t.Fatalf("30 should be allowed: %v", err)
	}
	if err := Validate(31); err != ErrTooHigh {
		t.Fatalf("31 should be rejected, got %v", err)
	}
}
