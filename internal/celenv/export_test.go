package celenv

import "cel.dev/cel-go/cel"

// SetCostLimit lowers the cost limit for a test of the limit itself; Envs
// built afterwards use it. It returns a function that restores the limit.
func SetCostLimit(n uint64) func() {
	old := costLimit
	costLimit = n
	return func() { costLimit = old }
}

// TypeCheck type-checks expr as a boolean on e, as ValidateRules does on the shared checker.
func (e *Env) TypeCheck(expr string) error {
	_, err := e.check("test\x00"+expr, expr, cel.BoolType)
	return err
}
