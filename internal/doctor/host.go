package doctor

import (
	"context"
	"time"
)

// Host contains bounded read-only dependencies. Inventory caches are audit-local.
// Callers must not mutate its function fields while an audit is running.
type Host struct {
	OS, Home, Path string
	Env            map[string]string
	Now            func() time.Time
	RunRead        func(context.Context, Command) (CommandResult, error)
	Fetch          func(context.Context, string) ([]byte, error)
}
