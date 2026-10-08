package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrMaintenanceActive indicates another active process holds the exclusive maintenance lock.
var ErrMaintenanceActive = errors.New("app: another maintenance process is currently active")

// ErrUncertainOwner indicates an unclean previous maintenance exit or orphan marker was detected.
var ErrUncertainOwner = errors.New("app: previous maintenance owner was not cleanly released; manual recovery check required")

// Ownership represents an exclusively held maintenance capability.
type Ownership interface {
	Token() string
	Release() error
}

type ownershipRecord struct {
	PID       int
	Token     string
	CreatedAt string
}

type fileOwnership struct {
	token      string
	lockFile   *os.File
	markerPath string
}

func (o *fileOwnership) Token() string {
	return o.token
}

func (o *fileOwnership) Release() error {
	var errs []error
	if o.markerPath != "" {
		if err := os.Remove(o.markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove owner marker: %w", err))
		}
	}
	if o.lockFile != nil {
		if err := unlockFile(o.lockFile); err != nil {
			errs = append(errs, fmt.Errorf("unlock kernel lock: %w", err))
		}
		if err := o.lockFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close lock file: %w", err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// AcquireOwnership acquires a kernel advisory lock and validates that no orphan owner exists.
func AcquireOwnership(ctx context.Context, stateDir string) (Ownership, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire ownership: %w", err)
	}
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("state directory is required for ownership")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	lockPath := filepath.Join(stateDir, "maintenance.lock")
	markerPath := filepath.Join(stateDir, "maintenance.owner")

	// Open or create the lock file with restrictive permissions.
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	// Try kernel non-blocking exclusive advisory lock.
	locked, err := tryLockFile(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	if !locked {
		_ = file.Close()
		return nil, ErrMaintenanceActive
	}

	// We hold the kernel lock. Now check if an owner marker file was left behind from a previous crash/orphan.
	markerData, err := os.ReadFile(markerPath)
	if err == nil && len(markerData) > 0 {
		// A leftover marker file exists while we just took the lock!
		// That means a previous process crashed or terminated without calling Release().
		// We cannot prove orphan updater work stopped, so fail-closed.
		_ = unlockFile(file)
		_ = file.Close()
		return nil, ErrUncertainOwner
	}

	// Generate a secure random token.
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		_ = unlockFile(file)
		_ = file.Close()
		return nil, fmt.Errorf("generate ownership token: %w", err)
	}
	token := hex.EncodeToString(randomBytes)

	// Write ownership marker.
	record := fmt.Sprintf("%d\n%s\n%s\n", os.Getpid(), token, time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(markerPath, []byte(record), 0o600); err != nil {
		_ = unlockFile(file)
		_ = file.Close()
		return nil, fmt.Errorf("write owner marker: %w", err)
	}

	return &fileOwnership{
		token:      token,
		lockFile:   file,
		markerPath: markerPath,
	}, nil
}
