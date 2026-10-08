package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceOwnership(t *testing.T) {
	stateDir := t.TempDir()
	own1, err := AcquireOwnership(t.Context(), stateDir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if own1.Token() == "" {
		t.Fatal("empty ownership token")
	}
	_, err = AcquireOwnership(t.Context(), stateDir)
	if !errors.Is(err, ErrMaintenanceActive) {
		t.Fatalf("second acquire error=%v, want ErrMaintenanceActive", err)
	}
	if err := own1.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	own2, err := AcquireOwnership(t.Context(), stateDir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := own2.Release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

func TestMaintenanceOwnershipDifferentDatabasePath(t *testing.T) {
	// The stable ownership directory is user-level, independent of any database override.
	stateDir := t.TempDir()
	own, err := AcquireOwnership(t.Context(), stateDir)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = own.Release() }()

	// An instance with a different database path pointing to the same stateDir must still be blocked
	_, err = AcquireOwnership(t.Context(), stateDir)
	if !errors.Is(err, ErrMaintenanceActive) {
		t.Fatalf("acquire with same stateDir: got %v, want ErrMaintenanceActive", err)
	}
}

func TestMaintenanceOwnershipHelperProcess(t *testing.T) {
	if os.Getenv("WORKSTATION_DOCTOR_TEST_HELPER") == "hold-lock" {
		stateDir := os.Getenv("WORKSTATION_DOCTOR_TEST_STATE_DIR")
		own, err := AcquireOwnership(context.Background(), stateDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper acquire error: %v\n", err)
			os.Exit(2)
		}
		readyFile := os.Getenv("WORKSTATION_DOCTOR_TEST_READY_FILE")
		if err := os.WriteFile(readyFile, []byte(own.Token()), 0o600); err != nil {
			os.Exit(3)
		}
		time.Sleep(10 * time.Second)
		_ = own.Release()
		os.Exit(0)
	}

	stateDir := t.TempDir()
	readyFile := filepath.Join(stateDir, "ready.txt")
	cmd := exec.Command(os.Args[0], "-test.run=TestMaintenanceOwnershipHelperProcess")
	cmd.Env = append(os.Environ(),
		"WORKSTATION_DOCTOR_TEST_HELPER=hold-lock",
		"WORKSTATION_DOCTOR_TEST_STATE_DIR="+stateDir,
		"WORKSTATION_DOCTOR_TEST_READY_FILE="+readyFile,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	for range 50 {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Active helper holds kernel lock: acquiring must return ErrMaintenanceActive
	_, err := AcquireOwnership(t.Context(), stateDir)
	if !errors.Is(err, ErrMaintenanceActive) {
		t.Fatalf("expected ErrMaintenanceActive while helper holds lock, got %v", err)
	}

	// Kill helper to simulate sudden exit
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	// Once killed, lock fd is released by kernel, but marker remains without clean release.
	// Must fail-closed with ErrUncertainOwner!
	_, err = AcquireOwnership(t.Context(), stateDir)
	if !errors.Is(err, ErrUncertainOwner) {
		t.Fatalf("expected ErrUncertainOwner after unclean helper termination, got %v", err)
	}
}

func TestUncertainOwnerBlocksMaintenance(t *testing.T) {
	stateDir := t.TempDir()
	record := ownershipRecord{
		PID:       999999,
		Token:     "orphan-token",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	markerPath := filepath.Join(stateDir, "maintenance.owner")
	data := fmt.Sprintf("%d\n%s\n%s\n", record.PID, record.Token, record.CreatedAt)
	if err := os.WriteFile(markerPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := AcquireOwnership(t.Context(), stateDir)
	if !errors.Is(err, ErrUncertainOwner) {
		t.Fatalf("AcquireOwnership on orphan marker returned %v, want ErrUncertainOwner", err)
	}
}
