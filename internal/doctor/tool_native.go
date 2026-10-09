package doctor

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

func discoverNativeTool(ctx context.Context, host *Host, scope Scope, id string, defaults []string) Discovery {
	d := ExecutableCandidates(ctx, host, scope, id, id, defaults)
	for n := range d.Instances {
		i := &d.Instances[n]
		i.Root = Fact{State: EvidenceUnavailable, Label: "Installation root", Source: "executable discovery", ObservedAt: hostNow(host)}
		i.Version = Fact{State: EvidenceUnavailable, Label: "Installed version", Source: "local metadata", Note: "No safe supported local version reader is available for this layout.", ObservedAt: hostNow(host)}
		i.Provenance = Provenance{State: EvidenceUnavailable}
	}
	return attachBrewEvidence(ctx, host, scope, d, id)
}

func checkGithubTool(ctx context.Context, host *Host, instance Instance, id, repo string) []Finding {
	key := FindingKey{IntegrationID: id, CheckID: id, InstanceID: instance.ID}
	reference := "https://github.com/" + repo
	f := Finding{Key: key, Outcome: OutcomeUnknown, Question: "Is a supported update available?", Explanation: "Manager availability is unknown. Use the installation source for manual maintenance.", Evidence: []Fact{instance.Version}, References: []PublicReference{{Kind: "documentation", Label: "Official project", URL: reference}}, Actions: []ActionProposal{{ID: "manual-maintenance", Key: key, Mode: ActionManual, Label: "Review installation-source guidance", Reason: "No supported automatic update decision is established.", DisabledReason: "Automatic maintenance is unavailable for this installation source."}}}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		return []Finding{f}
	}
	if host == nil || host.Fetch == nil || instance.Version.State != EvidenceKnown {
		return []Finding{f}
	}
	raw, err := host.Fetch(ctx, "https://api.github.com/repos/"+repo+"/releases/latest")
	var release struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err != nil || len(raw) > int(DefaultLimits().MaxHTTPBytes) || json.Unmarshal(raw, &release) != nil || release.Prerelease || release.Draft || release.TagName == "" {
		// Fallback to tags when releases/latest is not published (e.g. ghostty)
		rawTags, errTags := host.Fetch(ctx, "https://api.github.com/repos/"+repo+"/tags")
		if errTags != nil || len(rawTags) > int(DefaultLimits().MaxHTTPBytes) {
			f.Explanation = "Upstream lookup is unavailable. Installed version evidence is retained."
			if ctx.Err() != nil {
				f.Outcome = OutcomeCanceled
			}
			return []Finding{f}
		}
		var tags []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(rawTags, &tags) != nil || len(tags) == 0 {
			f.Explanation = "Upstream lookup is unavailable. Installed version evidence is retained."
			return []Finding{f}
		}
		release.TagName = tags[0].Name
		release.HTMLURL = "https://github.com/" + repo + "/releases/tag/" + tags[0].Name
	}
	order, err := CompareVersions(instance.Version.Value, release.TagName, "semver")
	if err != nil {
		return []Finding{f}
	}
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Upstream stable release", Value: release.TagName, Source: "official GitHub release metadata", Note: "Not a manager update decision.", ObservedAt: hostNow(host)})
	if publicReleaseURL(release.HTMLURL, repo) {
		f.References = append(f.References, PublicReference{Kind: "release-notes", Label: "Official release notes", URL: release.HTMLURL})
	}
	if order < 0 {
		f.Outcome = OutcomeAttention
		f.Explanation = "A newer upstream release exists. Manager availability is not established."
	} else {
		f.Outcome = OutcomeOK
		f.Explanation = "The installed version is equal to or newer than the observed upstream release."
	}
	return []Finding{f}
}

func publicReleaseURL(address, repo string) bool {
	u, e := url.Parse(address)
	return e == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.HasPrefix(u.Path, "/"+repo+"/releases/tag/") && validText(address, DefaultLimits().MaxPathBytes)
}
