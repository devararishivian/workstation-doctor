package doctor

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// CompareVersions compares supported semantic versions; it never infers manager availability.
func CompareVersions(installed, candidate, scheme string) (int, error) {
	if scheme != "semver" {
		return 0, errors.New("version scheme is unsupported")
	}
	installed = canonicalSemver(installed)
	candidate = canonicalSemver(candidate)
	if !semver.IsValid(installed) {
		return 0, fmt.Errorf("parse installed version: invalid semantic version")
	}
	if !semver.IsValid(candidate) {
		return 0, fmt.Errorf("parse candidate version: invalid semantic version")
	}
	return semver.Compare(installed, candidate), nil
}

func canonicalSemver(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if value[0] != 'v' && value[0] != 'V' {
		value = "v" + value
	} else if value[0] == 'V' {
		value = "v" + value[1:]
	}
	return value
}
