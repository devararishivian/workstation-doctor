package doctor

import (
	"errors"
	"time"
)

// Limits bounds read-only inspection and the size of represented action plans.
type Limits struct {
	MaxConcurrency, MaxCaptureBytes, MaxFiles, MaxDepth           int
	MaxSteps, MaxTargets, MaxArguments, MaxArgumentBytes          int
	MaxIdentifierBytes, MaxLabelBytes, MaxNoteBytes, MaxPathBytes int
	MaxFileBytes, MaxHTTPBytes                                    int64
	InspectionTimeout, HTTPTimeout, AuditTimeout                  time.Duration
	MaintenanceTimeout, FinalizationTimeout                       time.Duration
}

// DefaultLimits returns the reviewed initial workload limits.
func DefaultLimits() Limits {
	return Limits{
		MaxConcurrency: 8, MaxCaptureBytes: 1 << 20, MaxFiles: 10000, MaxDepth: 32,
		MaxSteps: 32, MaxTargets: 128, MaxArguments: 128, MaxArgumentBytes: 32 << 10,
		MaxIdentifierBytes: 256, MaxLabelBytes: 2 << 10, MaxNoteBytes: 4 << 10, MaxPathBytes: 4 << 10,
		MaxFileBytes: 2 << 20, MaxHTTPBytes: 2 << 20,
		InspectionTimeout: 30 * time.Second, HTTPTimeout: 15 * time.Second, AuditTimeout: 2 * time.Minute,
		MaintenanceTimeout: 10 * time.Minute, FinalizationTimeout: 5 * time.Second,
	}
}

func (l Limits) validate() error {
	for _, n := range []int{
		l.MaxConcurrency, l.MaxCaptureBytes, l.MaxFiles, l.MaxDepth, l.MaxSteps, l.MaxTargets,
		l.MaxArguments, l.MaxArgumentBytes, l.MaxIdentifierBytes, l.MaxLabelBytes, l.MaxNoteBytes, l.MaxPathBytes,
	} {
		if n <= 0 {
			return errors.New("inspection and action limits must be positive")
		}
	}
	if l.MaxFileBytes <= 0 || l.MaxHTTPBytes <= 0 {
		return errors.New("read limits must be positive")
	}
	for _, d := range []time.Duration{l.InspectionTimeout, l.HTTPTimeout, l.AuditTimeout, l.MaintenanceTimeout, l.FinalizationTimeout} {
		if d <= 0 {
			return errors.New("operation deadlines must be positive")
		}
	}
	return nil
}
