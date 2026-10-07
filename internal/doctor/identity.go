package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validIdentifier(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func validText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

// CanonicalInstanceID deduplicates established path aliases, not equal versions.
// Missing explicit locations remain errors instead of silently using defaults.
func CanonicalInstanceID(integrationID, scope, path string) (string, error) {
	limits := DefaultLimits()
	if !validIdentifier(integrationID, limits.MaxIdentifierBytes) || strings.TrimSpace(scope) == "" || path == "" {
		return "", errors.New("instance identity requires integration, scope, and location")
	}
	if strings.ContainsRune(scope, 0) || strings.ContainsRune(path, 0) || len(path) > limits.MaxPathBytes || len(scope) > limits.MaxPathBytes {
		return "", errors.New("instance identity scope or location is invalid")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute instance location: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve instance location: %w", err)
	}
	sum := sha256.Sum256([]byte(integrationID + "\x00" + scope + "\x00" + filepath.Clean(resolved)))
	id := integrationID + ":" + hex.EncodeToString(sum[:])
	if len(id) > limits.MaxIdentifierBytes {
		return "", errors.New("instance identity exceeds byte limit")
	}
	return id, nil
}
