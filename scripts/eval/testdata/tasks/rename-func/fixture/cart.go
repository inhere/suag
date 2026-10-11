package cart

// ComputeTotal adds up the item prices in cents.
func ComputeTotal(prices []int) int {
	t := 0
	for _, p := range prices {
		t += p
	}
	return t
}
