package stats

import "testing"

func TestSum(t *testing.T) {
	if got := Sum([]int{1, 2, 3}); got != 6 {
		t.Fatalf("Sum = %d, want 6", got)
	}
}

func TestAverage(t *testing.T) {
	if got := Average([]int{2, 4, 6}); got != 4 {
		t.Fatalf("Average = %v, want 4", got)
	}
	if got := Average(nil); got != 0 {
		t.Fatalf("Average(nil) = %v, want 0", got)
	}
}
