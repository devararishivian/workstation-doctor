package doctor

import (
	"context"
)

// Status of one component.
const (
	StatusOK      = "OK"
	StatusUpdate  = "UPDATE"
	StatusUnknown = "UNKNOWN"
)

// Category represents the audit component category.
type Category string

// Audit component categories.
const (
	CategoryTool   Category = "tool"
	CategoryConfig Category = "config"
	CategorySystem Category = "system"
	CategorySkill  Category = "skill"
)

// Checker is the strategy interface for an audit component.
type Checker interface {
	Name() string
	Category() Category
	Check(ctx context.Context) Result
}

// Result is the check result of one component.
type Result struct {
	Component string
	Installed string
	Latest    string
	Status    string
	Note      string
	// Manual is a manual step ready to copy and paste (only for UPDATE).
	Manual string
	// Fix is the command that automatic mode runs (only for UPDATE).
	Fix string
}

// Summary counts each status.
type Summary struct {
	OK, Update, Unknown int
}

// Summarize counts the results by status.
func Summarize(results []Result) Summary {
	var s Summary
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			s.OK++
		case StatusUpdate:
			s.Update++
		default:
			s.Unknown++
		}
	}
	return s
}

// Pending returns results with status UPDATE (action is pending).
func Pending(results []Result) []Result {
	out := []Result{}
	for _, r := range results {
		if r.Status == StatusUpdate {
			out = append(out, r)
		}
	}
	return out
}

func ok(component, installed, latest, note string) Result {
	return Result{Component: component, Installed: installed, Latest: latest, Status: StatusOK, Note: note}
}

func needUpdate(component, installed, latest, manual, fix, note string) Result {
	return Result{
		Component: component, Installed: installed, Latest: latest,
		Status: StatusUpdate, Note: note, Manual: manual, Fix: fix,
	}
}

func unknown(component, installed, latest, note string) Result {
	return Result{Component: component, Installed: installed, Latest: latest, Status: StatusUnknown, Note: note}
}
