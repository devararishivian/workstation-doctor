package doctor

import (
	"context"
	"sync"
)

// Engine manages checkers and coordinates concurrent execution.
type Engine struct {
	checkers []Checker
}

// NewEngine creates an Engine initialized with all standard audit checkers.
func NewEngine() *Engine {
	return &Engine{
		checkers: []Checker{
			&piVersionChecker{},
			&herdrVersionChecker{},
			&ghosttyVersionChecker{},
			&starshipVersionChecker{},
			newNpmPackageChecker("opencode", "opencode-ai"),
			newNpmPackageChecker("tokenjuice", "tokenjuice"),
			&serenaVersionChecker{},
			&gortexVersionChecker{},
			&ghosttyConfigValidChecker{},
			&ghosttyConfigVersionChecker{},
			&herdrConfigValidChecker{},
			&herdrConfigVersionChecker{},
			&piConfigValidChecker{},
			&piConfigVersionChecker{},
			&brewOutdatedChecker{},
			&npmOutdatedGlobalChecker{},
			&piPackagesChecker{},
			&superpowersChecker{},
			&herdrIntegrationsChecker{},
			&herdrPluginsChecker{},
			&skillsChecker{},
		},
	}
}

// Run executes all checkers concurrently and returns the results preserving order.
func (e *Engine) Run(ctx context.Context) []Result {
	results := make([]Result, len(e.checkers))
	var wg sync.WaitGroup
	for i, chk := range e.checkers {
		wg.Add(1)
		go func(idx int, c Checker) {
			defer wg.Done()
			results[idx] = c.Check(ctx)
		}(i, chk)
	}
	wg.Wait()
	return results
}

// Run executes all checks concurrently and returns the results.
// It serves as a facade for NewEngine().Run(ctx).
func Run(ctx context.Context) []Result {
	return NewEngine().Run(ctx)
}
