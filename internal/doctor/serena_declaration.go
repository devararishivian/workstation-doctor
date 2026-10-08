package doctor

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
)

// discoverSerenaDeclaration returns aggregate-only declaration evidence.
// Parser errors and all server fields remain private and never enter diagnostics.
func discoverSerenaDeclaration(ctx context.Context, host *Host, path string) Discovery {
	d := Discovery{Availability: AvailabilityUndetermined}
	if !filepath.IsAbs(path) {
		d.Diagnostics = []string{"Explicit declaration location is not absolute."}
		return d
	}
	raw, e := ReadBounded(ctx, path, DefaultLimits().MaxFileBytes)
	if e != nil {
		d.Diagnostics = []string{"Declared capability could not be inspected."}
		return d
	}
	var config struct {
		Servers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &config) != nil || len(config.Servers) > DefaultLimits().MaxFiles {
		d.Diagnostics = []string{"Declaration format is invalid or exceeds limits."}
		return d
	}
	count := 0
	for _, server := range config.Servers {
		if len(server.Args) > DefaultLimits().MaxArguments {
			d.Diagnostics = []string{"Declaration arguments exceed inspection limits."}
			return d
		}
		match := filepath.Base(server.Command) == "serena"
		if filepath.Base(server.Command) == "uvx" || filepath.Base(server.Command) == "uv" {
			for _, arg := range server.Args {
				if arg == "serena" {
					match = true
				}
			}
		}
		if match {
			count++
		}
	}
	if count == 0 {
		d.Availability = AvailabilityAbsent
		return d
	}
	id, e := CanonicalInstanceID("serena", "user", path)
	if e != nil {
		d.Diagnostics = []string{"Declaration identity is unavailable."}
		return d
	}
	d.Instances = []Instance{{ID: id, IntegrationID: "serena", Scope: "user", Availability: AvailabilityUndetermined, DiscoverySource: "aggregate launch declarations", ObservedAt: hostNow(host), Executable: Fact{State: EvidenceUnavailable, Label: "Persistent executable", Note: "A declaration does not prove a runnable environment."}, Version: Fact{State: EvidenceUnavailable, Label: "Installed version", Source: "aggregate declarations", ObservedAt: hostNow(host)}, Provenance: Provenance{State: EvidenceUnavailable}, Configuration: []Fact{{State: EvidenceKnown, Label: "Declared capability count", Value: strconv.Itoa(count), Source: "aggregate declarations", ObservedAt: hostNow(host)}}}}
	return d
}
