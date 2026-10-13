package bgtest

// Add returns the sum of a and b.
func Add(a, b int) int { return a + b }

// Mul returns the product of a and b, computed by repeated addition so that
// negative factors work too.
func Mul(a, b int) int {
	neg := b < 0
	if neg {
		b = -b
	}
	out := 0
	for i := 0; i < b; i++ {
		out = Add(out, a)
	}
	if neg {
		return -out
	}
	return out
}
