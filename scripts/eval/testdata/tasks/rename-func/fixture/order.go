package cart

// OrderTotal is the total of an order including a flat shipping fee.
func OrderTotal(prices []int, shipping int) int {
	return ComputeTotal(prices) + shipping
}
