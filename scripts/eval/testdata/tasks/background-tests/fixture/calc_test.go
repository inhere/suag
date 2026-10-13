package bgtest

import (
	"testing"
	"time"
)

// TestSlow stands for a slow suite: it takes about 15 seconds.
func TestSlow(t *testing.T) {
	time.Sleep(15 * time.Second)
	if Add(2, 3) != 5 || Mul(-3, 4) != -12 || Mul(3, -4) != -12 {
		t.Fatal("calc is wrong")
	}
}
