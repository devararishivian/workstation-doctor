package doctor

import (
	"testing"

	"golang.org/x/mod/semver"
)

func canonicalVersion(version string) string {
	if version == "" {
		return version
	}
	if version[0] != 'v' && version[0] != 'V' {
		return "v" + version
	}
	if version[0] == 'V' {
		return "v" + version[1:]
	}
	return version
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, installed, candidate string
		want                       int
		wantErr                    bool
	}{
		{name: "leading v", installed: "v1.2.3", candidate: "1.2.4", want: -1},
		{name: "prerelease ordering", installed: "1.2.3-alpha.2", candidate: "1.2.3-alpha.10", want: -1},
		{name: "local newer", installed: "2.0.0", candidate: "1.9.9", want: 1},
		{name: "equal", installed: "1.2.3", candidate: "1.2.3", want: 0},
		{name: "invalid installed", installed: "main", candidate: "1.2.3", wantErr: true},
		{name: "invalid candidate", installed: "1.0.0", candidate: "1.0.0.0", wantErr: true},
		{name: "unknown scheme", installed: "1.0.0", candidate: "1.0.1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := "semver"
			if tt.name == "unknown scheme" {
				scheme = "homebrew"
			}
			got, err := CompareVersions(tt.installed, tt.candidate, scheme)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("CompareVersions()=(%d,%v), want (%d,error=%v)", got, err, tt.want, tt.wantErr)
			}
			if err == nil {
				a := canonicalVersion(tt.installed)
				b := canonicalVersion(tt.candidate)
				want := semver.Compare(a, b)
				if got < 0 && want >= 0 || got > 0 && want <= 0 || got == 0 && want != 0 {
					t.Fatalf("semver oracle=%d, implementation=%d", want, got)
				}
			}
		})
	}
}
