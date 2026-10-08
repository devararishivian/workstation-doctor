package doctor

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

func discoverGhostty(ctx context.Context, host *Host, scope Scope) Discovery {
	var defaults []string
	if host != nil && host.OS == "darwin" {
		defaults = []string{"/Applications/Ghostty.app/Contents/MacOS", filepath.Join(host.Home, "Applications", "Ghostty.app", "Contents", "MacOS")}
	}
	d := discoverNativeTool(ctx, host, scope, "ghostty", defaults)
	if host == nil || host.OS != "darwin" {
		return d
	}
	for n := range d.Instances {
		i := &d.Instances[n]
		root := filepath.Dir(filepath.Dir(i.ResolvedPath.Value))
		if filepath.Base(filepath.Dir(i.ResolvedPath.Value)) != "MacOS" || filepath.Base(root) != "Contents" || !strings.HasSuffix(filepath.Dir(root), ".app") {
			continue
		}
		raw, e := ReadBounded(ctx, filepath.Join(root, "Info.plist"), DefaultLimits().MaxFileBytes)
		if e != nil {
			i.Diagnostics = append(i.Diagnostics, "Application version metadata is unavailable.")
			continue
		}
		version, e := ghosttyPlistVersion(raw)
		if e != nil {
			i.Diagnostics = append(i.Diagnostics, "Application version metadata format is unsupported.")
			continue
		}
		i.Root = Fact{State: EvidenceKnown, Label: "Application bundle", Value: filepath.Dir(root), Source: "observed application layout", ObservedAt: hostNow(host)}
		i.Version = Fact{State: EvidenceKnown, Label: "Installed version", Value: version, Source: "Ghostty Info.plist", ObservedAt: hostNow(host)}
		// An application bundle does not prove Homebrew cask ownership.
	}
	return d
}

func checkGhosttyInstance(ctx context.Context, host *Host, _ Scope, instance Instance) []Finding {
	return checkGithubTool(ctx, host, instance, "ghostty", "ghostty-org/ghostty")
}

func ghosttyPlistVersion(raw []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	key := ""
	version := ""
	identifier := ""
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errors.New("application plist is invalid")
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if depth > DefaultLimits().MaxDepth {
				return "", errors.New("application plist exceeds depth limit")
			}
			if element.Name.Local == "key" {
				if decoder.DecodeElement(&key, &element) != nil {
					return "", errors.New("application plist key is invalid")
				}
				depth--
			}
			if element.Name.Local == "string" {
				var value string
				if decoder.DecodeElement(&value, &element) != nil {
					return "", errors.New("application plist value is invalid")
				}
				depth--
				if key == "CFBundleShortVersionString" {
					version = value
				}
				if key == "CFBundleIdentifier" {
					identifier = value
				}
				key = ""
			}
		case xml.EndElement:
			depth--
		}
	}
	if identifier != "com.mitchellh.ghostty" || !semver.IsValid(canonicalSemver(version)) {
		return "", errors.New("application identity or version is unsupported")
	}
	return version, nil
}
