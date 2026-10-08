package store

import "time"

// History schema version 2 adds stable timestamp ordering and filter indexes.
const (
	historySchemaVersion = 2
	historyApplicationID = 0x57534452 // "WSDR"
)

const (
	maxActionSteps      = 32
	maxActionTargets    = 128
	maxIdentifierBytes  = 256
	maxLabelBytes       = 2 << 10
	maxNoteBytes        = 4 << 10
	maxPathBytes        = 4 << 10
	maxSafeErrorBytes   = 4 << 10
	maxMetadataHard     = 8 << 10
	maxStepOutputHard   = 16 << 10
	maxActionOutputHard = 64 << 10
)

// Policy bounds local history retention and retained display excerpts.
type Policy struct {
	MaxAge          time.Duration
	MaxTerminal     int
	MaxStepOutput   int
	MaxActionOutput int
	MaxMetadata     int
}

// DefaultPolicy returns the approved bounded action-history defaults.
func DefaultPolicy() Policy {
	return Policy{
		MaxAge: 90 * 24 * time.Hour, MaxTerminal: 10_000,
		MaxStepOutput: maxStepOutputHard, MaxActionOutput: maxActionOutputHard,
		MaxMetadata: maxMetadataHard,
	}
}

// DBKind describes a history file without creating or changing it.
type DBKind string

// DBKind values distinguish absent, new, legacy, and unsupported history files.
const (
	DBKindMissing     DBKind = "missing"
	DBKindActionOnly  DBKind = "action-only"
	DBKindLegacy      DBKind = "legacy"
	DBKindUnsupported DBKind = "unsupported"
)

// ErrMigrationRequired marks a recognized legacy database that needs explicit import.
var ErrMigrationRequired = errHistoryMigrationRequired{}

// ErrUnsupportedSchema marks a file or database whose schema cannot be safely used.
var ErrUnsupportedSchema = errHistoryUnsupportedSchema{}

type errHistoryMigrationRequired struct{}

func (errHistoryMigrationRequired) Error() string {
	return "legacy history requires explicit migration"
}

type errHistoryUnsupportedSchema struct{}

func (errHistoryUnsupportedSchema) Error() string { return "history database schema is unsupported" }

// ActionStart is the bounded historical context saved before maintenance begins.
// Plan contains display descriptions only, never executable arguments.
type ActionStart struct {
	ID, BatchID, IntegrationID, CheckID, InstanceID string
	Label, Kind, Reason, AppVersion                 string
	InstalledVersion, TargetVersion, Manager        string
	Root, Scope, OwnerToken                         string
	StartedAt                                       time.Time
	Plan                                            []PlannedStep
	TargetIDs                                       []string
	MetadataVersion                                 int
	Metadata                                        map[string]string
}

// PlannedStep is safe display text for one ordered step, not an executable command.
type PlannedStep struct {
	Index       int
	Label       string
	Description string
}

// StepOutcome describes one persisted checkpoint.
type StepOutcome string

// StepOutcome values distinguish completed, failed, canceled, and unstarted steps.
const (
	StepOutcomeUnspecified StepOutcome = ""
	StepOutcomeCompleted   StepOutcome = "Completed"
	StepOutcomeFailed      StepOutcome = "Failed"
	StepOutcomeCanceled    StepOutcome = "Canceled"
	StepOutcomeNotStarted  StepOutcome = "NotStarted"
)

// StepResult contains a single bounded safe checkpoint.
type StepResult struct {
	Index           int
	Outcome         StepOutcome
	ExitCode        *int
	SafeError       string
	SafeOutput      string
	StartedAt       time.Time
	FinishedAt      time.Time
	OutputTruncated bool
}

// ExecutionOutcome describes whether the maintenance attempt executed.
type ExecutionOutcome string

// ExecutionOutcome values describe the final execution state of an attempt.
const (
	ExecutionUnspecified ExecutionOutcome = ""
	ExecutionCompleted   ExecutionOutcome = "Completed"
	ExecutionFailed      ExecutionOutcome = "Failed"
	ExecutionCanceled    ExecutionOutcome = "Canceled"
	ExecutionBlocked     ExecutionOutcome = "Blocked"
	ExecutionInterrupted ExecutionOutcome = "Interrupted"
)

// VerificationOutcome is distinct from command execution success.
type VerificationOutcome string

// VerificationOutcome values distinguish verified, failed, unknown, and omitted checks.
const (
	VerificationUnspecified  VerificationOutcome = ""
	VerificationPassed       VerificationOutcome = "Passed"
	VerificationFailed       VerificationOutcome = "Failed"
	VerificationUnknown      VerificationOutcome = "Unknown"
	VerificationNotPerformed VerificationOutcome = "NotPerformed"
)

// ActionFinish closes an attempt with independent execution and verification outcomes.
type ActionFinish struct {
	FinishedAt      time.Time
	Execution       ExecutionOutcome
	Verification    VerificationOutcome
	ObservedVersion string
	SafeError       string
}

// ActionRecord is a durable attempt; nil Finish means unfinished, not successful.
type ActionRecord struct {
	ActionStart
	Finish *ActionFinish
	Steps  []StepResult
}

// ActionQuery filters and paginates bounded action-history reads.
type ActionQuery struct {
	IntegrationID string
	InstanceID    string
	Execution     ExecutionOutcome
	Verification  VerificationOutcome
	BatchID       string
	From          *time.Time
	To            *time.Time
	Cursor        string
	Limit         int
}

// ActionPage is one newest-first page; list rows omit detailed step bodies.
type ActionPage struct {
	Items      []ActionRecord
	NextCursor string
}
