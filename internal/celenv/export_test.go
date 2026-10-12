package celenv

// SetCostLimit lowers the cost limit for a test of the limit itself; Envs
// built afterwards use it. It returns a function that restores the limit.
func SetCostLimit(n uint64) func() {
	old := costLimit
	costLimit = n
	return func() { costLimit = old }
}
