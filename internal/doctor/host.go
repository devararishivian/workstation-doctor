package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
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
	inventory      *auditInventory
}

// auditInventory owns discovery snapshots for one Audit or Inspect call only.
// Manager-specific inventory belongs here when its consumers are introduced.
type auditInventory struct{ discoveries []Discovery }

// NewHost captures the active environment without creating workstation state.
func NewHost(_ Scope, limits Limits) (*Host, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("determine inspection home: %w", err)
	}
	env := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	host := &Host{OS: runtime.GOOS, Home: home, Path: env["PATH"], Env: env, Now: time.Now, inventory: &auditInventory{}}
	processEnv := maps.Clone(env)
	host.RunRead = func(ctx context.Context, command Command) (CommandResult, error) {
		return runInspection(ctx, command, processEnv, limits)
	}
	client := &http.Client{Timeout: limits.HTTPTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("metadata redirect is unsupported")
		}
		return nil
	}}
	host.Fetch = func(ctx context.Context, address string) ([]byte, error) {
		return fetchMetadata(ctx, client, address, limits)
	}
	return host, nil
}

// ReadBounded reads a regular file with cancellation and an explicit byte limit.
func ReadBounded(ctx context.Context, path string, limit int64) (raw []byte, err error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read inspection file: %w", err)
	}
	if limit <= 0 || limit == math.MaxInt64 {
		return nil, errors.New("file byte limit is invalid")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect file location: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("inspection requires a regular file within byte limits")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open inspection file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	reader := io.LimitReader(file, limit+1)
	buffer := make([]byte, 32<<10)
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("read inspection file contents: %w", ctxErr)
		}
		n, readErr := reader.Read(buffer)
		raw = append(raw, buffer[:n]...)
		if int64(len(raw)) > limit {
			return nil, errors.New("inspection file exceeds byte limit")
		}
		if errors.Is(readErr, io.EOF) {
			return raw, nil
		}
		if readErr != nil {
			return nil, fmt.Errorf("read inspection file: %w", readErr)
		}
	}
}

func fetchMetadata(ctx context.Context, client *http.Client, address string, limits Limits) ([]byte, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("metadata URL requires HTTPS without credentials")
	}
	ctx, cancel := context.WithTimeout(ctx, limits.HTTPTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("create metadata request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request metadata: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck // A read-only response close cannot repair an inspection.
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata returned HTTP status %d", response.StatusCode)
	}
	if limits.MaxHTTPBytes <= 0 || limits.MaxHTTPBytes == math.MaxInt64 {
		return nil, errors.New("metadata byte limit is invalid")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limits.MaxHTTPBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read metadata body: %w", err)
	}
	if int64(len(body)) > limits.MaxHTTPBytes {
		return nil, errors.New("metadata exceeds response byte limit")
	}
	return body, nil
}

type inspectionCapture struct {
	mu             sync.Mutex
	remaining      int
	stdout, stderr bytes.Buffer
	truncated      bool
}

type inspectionWriter struct {
	capture *inspectionCapture
	stderr  bool
}

func (w inspectionWriter) Write(p []byte) (int, error) {
	c := w.capture
	c.mu.Lock()
	defer c.mu.Unlock()
	n := min(len(p), c.remaining)
	buffer := &c.stdout
	if w.stderr {
		buffer = &c.stderr
	}
	if _, err := buffer.Write(p[:n]); err != nil {
		return 0, fmt.Errorf("capture inspection output: %w", err)
	}
	c.remaining -= n
	if n < len(p) {
		c.truncated = true
	}
	return len(p), nil
}

func runInspection(ctx context.Context, command Command, environment map[string]string, limits Limits) (CommandResult, error) {
	result := CommandResult{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("start inspection command: %w", err)
	}
	if err := validateCommand(command, limits); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, limits.InspectionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Dir = command.Dir
	env := maps.Clone(environment)
	if env == nil {
		env = map[string]string{}
	}
	maps.Copy(env, command.Env)
	for _, key := range slices.Sorted(maps.Keys(env)) {
		cmd.Env = append(cmd.Env, key+"="+env[key])
	}
	capture := &inspectionCapture{remaining: limits.MaxCaptureBytes}
	cmd.Stdout = inspectionWriter{capture: capture}
	cmd.Stderr = inspectionWriter{capture: capture, stderr: true}
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	capture.mu.Lock()
	result.Stdout = bytes.Clone(capture.stdout.Bytes())
	result.Stderr = bytes.Clone(capture.stderr.Bytes())
	result.Truncated = capture.truncated
	capture.mu.Unlock()
	if err != nil {
		err = errors.Join(err, ctx.Err())
	}
	if result.Truncated {
		err = errors.Join(err, errors.New("inspection command output exceeded byte limit"))
	}
	if err != nil {
		return result, fmt.Errorf("run inspection command: %w", err)
	}
	return result, nil
}
