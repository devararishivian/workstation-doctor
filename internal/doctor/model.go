package doctor

import "time"

// Availability describes discovery, independently of a check's outcome.
type Availability string

// Discovery states never treat an unspecified value as success.
const (
	AvailabilityPresent      Availability = "Present"
	AvailabilityAbsent       Availability = "Absent"
	AvailabilityUnsupported  Availability = "Unsupported"
	AvailabilityUndetermined Availability = "Undetermined"
)

// Outcome is the answer to an inspection question.
type Outcome string

// Inspection outcomes separate attention, uncertainty, and applicability.
const (
	OutcomeOK            Outcome = "OK"
	OutcomeAttention     Outcome = "Attention"
	OutcomeUnknown       Outcome = "Unknown"
	OutcomeNotApplicable Outcome = "NotApplicable"
	OutcomeCanceled      Outcome = "Canceled"
)

// EvidenceState identifies the quality of an observed fact.
type EvidenceState string

// Evidence states preserve unknown values rather than inventing defaults.
const (
	EvidenceKnown       EvidenceState = "Known"
	EvidenceUnavailable EvidenceState = "Unavailable"
	EvidenceUnsupported EvidenceState = "UnsupportedEvidence"
	EvidenceReadFailed  EvidenceState = "ReadFailed"
)

// ActionMode separates automatic maintenance from guidance and inspection.
type ActionMode string

// Action modes determine whether confirmation and maintenance recording apply.
const (
	ActionAutomatic  ActionMode = "Automatic"
	ActionManual     ActionMode = "Manual"
	ActionInspection ActionMode = "Inspection"
)

// Scope contains explicitly selected resources; empty ProjectDir is user scope.
type Scope struct {
	ProjectDir string
	Locations  map[string][]string
	SkillRoots []string
}

// Fact is labeled evidence, not arbitrary command or configuration output.
type Fact struct {
	State                      EvidenceState
	Label, Value, Source, Note string
	ObservedAt                 time.Time
}

// InstallationEvidence records a trustworthy time with its actual meaning.
type InstallationEvidence struct {
	At                         time.Time
	Source, Meaning, Precision string
	ObservedAt                 time.Time
}

// Provenance links an instance to an established manager and installation scope.
type Provenance struct {
	State                           EvidenceState
	Manager, Package, Root, Channel string
}

// Integration describes supported behavior, never asserting installation.
type Integration struct {
	ID, Name, Description string
	References            []string
	SupportedScopes       []string
}

// PublicReference links established public metadata, never private MCP values.
type PublicReference struct{ Kind, Label, URL string }

// CheckDescriptor is derived from registration for generic details.
type CheckDescriptor struct {
	IntegrationID, ID, Name, Question string
	Order                             int
}

// Instance is an installation or declared resource within discovery scope.
type Instance struct {
	ID, IntegrationID                       string
	Scope, DiscoverySource                  string
	ObservedAt                              time.Time
	Capabilities                            []ActionMode
	Availability                            Availability
	Active                                  bool
	Executable, ResolvedPath, Root, Version Fact
	Configuration                           []Fact
	Provenance                              Provenance
	InstalledAt                             *InstallationEvidence
	Diagnostics                             []string
}

// FindingKey identifies a check of one specific discovered instance.
type FindingKey struct{ IntegrationID, CheckID, InstanceID string }

// Finding carries safe observations and proposed capabilities in memory.
type Finding struct {
	Key                   FindingKey
	Outcome               Outcome
	Question, Explanation string
	Evidence              []Fact
	References            []PublicReference
	Actions               []ActionProposal
}

// Command is executable data, separate from presentation and persistence.
// Its fields are deliberately excluded from incidental JSON encoding.
type Command struct {
	Executable string            `json:"-"`
	Dir        string            `json:"-"`
	Args       []string          `json:"-"`
	Env        map[string]string `json:"-"`
}

// CommandResult is raw internal inspection output, never a safe display value.
type CommandResult struct {
	Stdout    []byte `json:"-"`
	Stderr    []byte `json:"-"`
	ExitCode  int
	Truncated bool
}

// CommandStep contains one direct-argv operation with a safe label.
type CommandStep struct {
	Label   string
	Command Command
}

// ActionProposal describes a capability; it is not confirmation to execute it.
type ActionProposal struct {
	ID                            string
	Key                           FindingKey
	Mode                          ActionMode
	Label, Reason, DisabledReason string
	TargetIDs                     []string
	TargetVersion                 Fact
	Steps                         []CommandStep
	Preconditions                 []Fact
	VerificationCheckID           string
	SideEffects                   []string
}

// Discovery records presence and incomplete coverage even with no instances.
type Discovery struct {
	Availability Availability
	Instances    []Instance
	Diagnostics  []string
}

// AuditReport is the current in-memory observation, not a history snapshot.
type AuditReport struct {
	StartedAt, FinishedAt time.Time
	Scope                 Scope
	Integrations          []Integration
	Checks                []CheckDescriptor
	Discoveries           []Discovery
	Findings              []Finding
	Canceled              bool
}
