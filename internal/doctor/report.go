package doctor

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Status colors follow the Catppuccin palette. lipgloss renders them
// only on a capable terminal; piped output stays plain automatically.
var (
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	updateStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF"))
	unknownStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8"))
)

// FormatTable renders the results as a text table. It returns text
// instead of writing it, so callers route the report through the
// program-output logger and the output stays pipeable.
func FormatTable(results []Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-22s %-16s %-16s %-10s %s\n", "COMPONENT", "INSTALLED", "LATEST", "STATUS", "NOTE")
	fmt.Fprintln(&b, "----------------------------------------------------------------------------------------------")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-16s %-16s %-10s %s\n",
			trunc(r.Component, 22), trunc(r.Installed, 16), trunc(r.Latest, 16), r.Status, r.Note)
	}
	s := Summarize(results)
	fmt.Fprintf(&b, "\nSummary: %s · %s · %s\n",
		okStyle.Render(strconv.Itoa(s.OK)+" OK"),
		updateStyle.Render(strconv.Itoa(s.Update)+" need update"),
		unknownStyle.Render(strconv.Itoa(s.Unknown)+" unknown"))
	return b.String()
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// FormatManual renders the ordered manual steps for UPDATE results.
// It returns text instead of writing it, for the same reason as FormatTable.
func FormatManual(results []Result) string {
	pending := Pending(results)
	var b strings.Builder
	fmt.Fprintln(&b, "\n== Ordered manual steps ==")
	if len(pending) == 0 {
		fmt.Fprintln(&b, "No action is needed. All components are current.")
		return b.String()
	}
	for i, r := range pending {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r.Manual)
	}
	return b.String()
}
