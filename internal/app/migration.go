package app

import (
	"context"
	"fmt"

	"github.com/devararishivian/workstation-doctor/internal/store"
)

// PreviewMigration generates a read-only preview of actions eligible for legacy migration.
func (s *Service) PreviewMigration(ctx context.Context, source, destination string) (store.MigrationPreview, error) {
	preview, err := store.PreviewLegacyMigration(ctx, source, destination)
	if err != nil {
		return store.MigrationPreview{}, fmt.Errorf("preview legacy migration: %w", err)
	}
	return preview, nil
}

// MigrateLegacy executes migration only when user confirmation is explicit and exclusive ownership is held.
func (s *Service) MigrateLegacy(ctx context.Context, preview store.MigrationPreview, confirmed bool) (store.MigrationResult, error) {
	if !confirmed {
		return store.MigrationResult{}, nil
	}

	ownership, err := s.options.Acquire(ctx, s.options.StateDir)
	if err != nil {
		return store.MigrationResult{}, fmt.Errorf("acquire migration ownership: %w", err)
	}
	defer func() { _ = ownership.Release() }()

	result, err := store.ImportLegacyActions(ctx, preview)
	if err != nil {
		return store.MigrationResult{}, fmt.Errorf("import legacy actions: %w", err)
	}
	return result, nil
}
