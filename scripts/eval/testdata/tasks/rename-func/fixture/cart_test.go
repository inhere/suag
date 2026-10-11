package cart

import "testing"

func TestComputeTotal(t *testing.T) {
	if got := ComputeTotal([]int{100, 250}); got != 350 {
		t.Fatalf("got %d", got)
	}
}

func TestOrderTotal(t *testing.T) {
	if got := OrderTotal([]int{100, 250}, 50); got != 400 {
		t.Fatalf("got %d", got)
	}
}
