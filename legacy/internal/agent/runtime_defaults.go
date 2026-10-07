package agent

func normalizeMaxSteps(maxSteps int) int {
	if maxSteps <= 0 {
		return defaultMaxSteps
	}
	return maxSteps
}
