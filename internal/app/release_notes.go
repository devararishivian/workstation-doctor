package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/devararishivian/workstation-doctor/internal/doctor"
)

// ReleaseNotes carries bounded, sanitized release notes text retrieved from a public reference.
type ReleaseNotes struct {
	URL        string
	Text       string
	ObservedAt time.Time
	Truncated  bool
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func sanitizeReleaseNotes(raw []byte, maxBytes int) (string, bool) {
	text := ansiRegex.ReplaceAllString(string(raw), "")
	// Clean non-printable control characters except newline and tab
	var sb strings.Builder
	for _, r := range text {
		if r == '\n' || r == '	' || (r >= 32 && r != 127) {
			sb.WriteRune(r)
		}
	}
	cleaned := sb.String()
	truncated := false
	if len(cleaned) > maxBytes {
		cleaned = cleaned[:maxBytes]
		truncated = true
	}
	return cleaned, truncated
}

// ReleaseNotes retrieves and sanitizes release notes for the specified finding.
func (s *Service) ReleaseNotes(ctx context.Context, key doctor.FindingKey) (ReleaseNotes, error) {
	finding, err := s.engine.Inspect(ctx, s.host, s.scope, key)
	if err != nil {
		return ReleaseNotes{}, fmt.Errorf("inspect finding: %w", err)
	}

	var notesURL string
	for _, ref := range finding.References {
		if ref.Kind == "release-notes" && ref.URL != "" {
			notesURL = ref.URL
			break
		}
	}
	if notesURL == "" {
		return ReleaseNotes{}, errors.New("finding has no release-notes reference")
	}

	data, err := s.host.Fetch(ctx, notesURL)
	if err != nil {
		return ReleaseNotes{}, fmt.Errorf("fetch release notes: %w", err)
	}

	const maxNoteBytes = 64 << 10 // 64 KiB
	text, truncated := sanitizeReleaseNotes(data, maxNoteBytes)

	return ReleaseNotes{
		URL:        notesURL,
		Text:       text,
		ObservedAt: time.Now().UTC(),
		Truncated:  truncated,
	}, nil
}
