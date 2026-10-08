package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

// Configuration paths are candidates, not reconstructed runtime CLI overrides.
func configurationFacts(host *Host, paths []string) ([]Fact, error) {
	if host == nil || !filepath.IsAbs(host.Home) {
		return nil, errors.New("configuration scope is unavailable")
	}
	if len(paths) == 0 || len(paths) > DefaultLimits().MaxFiles {
		return nil, errors.New("configuration paths exceed limits")
	}
	facts := make([]Fact, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p) || !validText(p, DefaultLimits().MaxPathBytes) {
			return nil, errors.New("configuration location is invalid")
		}
		facts = append(facts, Fact{State: EvidenceKnown, Label: "Configuration candidate", Value: filepath.Clean(p), Source: "documented product configuration location", Note: "Runtime CLI overrides are not reconstructed.", ObservedAt: hostNow(host)})
	}
	return facts, nil
}

func piAgentDirectory(h *Host) (string, error) {
	if h == nil {
		return "", errors.New("agent scope unavailable")
	}
	p := h.Env["PI_CODING_AGENT_DIR"]
	if p == "" {
		p = filepath.Join(h.Home, ".pi", "agent")
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("agent override is not absolute")
	}
	return p, nil
}

// ResolvePiConfiguration selects user and explicitly scoped project candidates.
func ResolvePiConfiguration(h *Host, s Scope) ([]Fact, error) {
	if paths, ok := s.Locations["pi-config"]; ok {
		return configurationFacts(h, paths)
	}
	root, e := piAgentDirectory(h)
	if e != nil {
		return nil, e
	}
	paths := []string{filepath.Join(root, "settings.json"), filepath.Join(root, "mcp.json"), filepath.Join(root, "mcp-adapter.json")}
	if s.ProjectDir != "" {
		if !filepath.IsAbs(s.ProjectDir) {
			return nil, errors.New("project scope is not absolute")
		}
		paths = append(paths, filepath.Join(s.ProjectDir, ".pi", "settings.json"), filepath.Join(s.ProjectDir, ".pi", "mcp.json"))
	}
	return configurationFacts(h, paths)
}

func configHome(h *Host) string {
	if p := h.Env["XDG_CONFIG_HOME"]; filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(h.Home, ".config")
}

// ResolveHerdrConfiguration honors explicit file and environment precedence.
func ResolveHerdrConfiguration(h *Host, s Scope) ([]Fact, error) {
	if paths, ok := s.Locations["herdr-config"]; ok {
		return configurationFacts(h, paths)
	}
	if h == nil {
		return nil, errors.New("configuration scope unavailable")
	}
	p := h.Env["HERDR_CONFIG_PATH"]
	if p == "" {
		p = filepath.Join(configHome(h), "herdr", "config.toml")
	}
	return configurationFacts(h, []string{p})
}

// ResolveGhosttyConfiguration preserves documented version-specific loading order.
func ResolveGhosttyConfiguration(h *Host, s Scope, version string) ([]Fact, error) {
	if paths, ok := s.Locations["ghostty-config"]; ok {
		return configurationFacts(h, paths)
	}
	if h == nil {
		return nil, errors.New("configuration scope unavailable")
	}
	names := []string{"config"}
	order, e := CompareVersions(version, "1.2.3", "semver")
	if e != nil || order >= 0 {
		names = []string{"config.ghostty", "config"}
	}
	roots := []string{filepath.Join(configHome(h), "ghostty")}
	if h.OS == "darwin" {
		roots = append(roots, filepath.Join(h.Home, "Library", "Application Support", "com.mitchellh.ghostty"))
	}
	var paths []string
	for _, root := range roots {
		for _, name := range names {
			paths = append(paths, filepath.Join(root, name))
		}
	}
	return configurationFacts(h, paths)
}

func configurationFinding(ctx context.Context, h *Host, i Instance, id string, paths []Fact, resolveErr error, format string) []Finding {
	f := Finding{Key: FindingKey{IntegrationID: i.IntegrationID, CheckID: id, InstanceID: i.ID}, Outcome: OutcomeOK, Question: "Is declared configuration syntax supported?", Explanation: "Optional configuration is absent; documented defaults apply."}
	if resolveErr != nil {
		f.Outcome = OutcomeUnknown
		f.Explanation = "Configuration location could not be established."
		return []Finding{f}
	}
	if len(paths) > DefaultLimits().MaxFiles {
		f.Outcome = OutcomeUnknown
		return []Finding{f}
	}
	for _, p := range paths {
		if ctx.Err() != nil {
			f.Outcome = OutcomeCanceled
			break
		}
		raw, e := ReadBounded(ctx, p.Value, DefaultLimits().MaxFileBytes)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			f.Outcome = OutcomeUnknown
			f.Explanation = "Configuration could not be read within inspection limits."
			break
		}
		// Do not return path-dependent parser errors or any configuration values.
		switch format {
		case "json":
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) != nil || object == nil {
				f.Outcome = OutcomeAttention
				f.Explanation = "Configuration is not a supported JSON object."
				break
			}
			if filepath.Base(p.Value) == "mcp.json" || filepath.Base(p.Value) == "mcp-adapter.json" {
				var servers map[string]json.RawMessage
				if rawServers, ok := object["mcpServers"]; ok {
					if json.Unmarshal(rawServers, &servers) != nil || len(servers) > DefaultLimits().MaxFiles {
						f.Outcome = OutcomeAttention
						f.Explanation = "MCP declaration structure is unsupported."
						break
					}
				}
				f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "MCP declaration count", Value: strconv.Itoa(len(servers)), Source: "aggregate configuration inspection", ObservedAt: hostNow(h)})
			}
			f.Explanation = "JSON object syntax is valid. Adapter applicability and runtime semantics are not established; no cache is required."
		case "toml":
			var object map[string]any
			if toml.Unmarshal(raw, &object) != nil {
				f.Outcome = OutcomeAttention
				f.Explanation = "Configuration TOML syntax is invalid."
				break
			}
			f.Explanation = "TOML syntax is valid. Optional omitted sections do not imply obsolete configuration; runtime semantics are not established."
		default:
			f.Outcome = OutcomeUnknown
			f.Explanation = "Ghostty configuration exists. Version-specific option semantics and included files require native diagnostics whose startup safety is not established."
		}
	}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
	}
	return []Finding{f}
}

func configCompatibility(ctx context.Context, i Instance, id string) []Finding {
	f := Finding{Key: FindingKey{IntegrationID: i.IntegrationID, CheckID: id, InstanceID: i.ID}, Outcome: OutcomeUnknown, Question: "Is configuration compatible with the installed version?", Explanation: "No supported static schema compatibility contract is established. Changelog presentation state and omitted optional sections are not schema versions.", Evidence: []Fact{i.Version}}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
	}
	return []Finding{f}
}

func checkPiConfigValid(c context.Context, h *Host, s Scope, i Instance) []Finding {
	p, e := ResolvePiConfiguration(h, s)
	return configurationFinding(c, h, i, "pi-config-valid", p, e, "json")
}

func checkPiConfigCompatibility(c context.Context, _ *Host, _ Scope, i Instance) []Finding {
	return configCompatibility(c, i, "pi-config-version")
}

func checkHerdrConfigValid(c context.Context, h *Host, s Scope, i Instance) []Finding {
	p, e := ResolveHerdrConfiguration(h, s)
	return configurationFinding(c, h, i, "herdr-config-valid", p, e, "toml")
}

func checkHerdrConfigCompatibility(c context.Context, _ *Host, _ Scope, i Instance) []Finding {
	return configCompatibility(c, i, "herdr-config-version")
}

func checkGhosttyConfigValid(c context.Context, h *Host, s Scope, i Instance) []Finding {
	p, e := ResolveGhosttyConfiguration(h, s, i.Version.Value)
	return configurationFinding(c, h, i, "ghostty-config-valid", p, e, "ghostty")
}

func checkGhosttyConfigCompatibility(c context.Context, _ *Host, _ Scope, i Instance) []Finding {
	return configCompatibility(c, i, "ghostty-config-version")
}
