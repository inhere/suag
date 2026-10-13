package watchtest

import (
	"testing"
	"time"
)

// TestCalc stands for a slow suite: it takes about 5 seconds.
func TestCalc(t *testing.T) {
	time.Sleep(5 * time.Second)
	if Add(2, 3) != 5 || Mul(3, 4) != 12 || Mul(3, -4) != -12 || Mul(-3, -4) != 12 {
		t.Fatal("calc is wrong")
	}
}
