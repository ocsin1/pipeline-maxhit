package report

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/ocsin1/pipeline-maxhit/internal/solver"
)

func Print(w io.Writer, results []solver.ExecResult, sccWarnings []string) {
	sorted := sortResults(results)
	printSummary(w, results, sccWarnings)
	printTable(w, sorted)
}

func sortResults(results []solver.ExecResult) []solver.ExecResult {
	sorted := make([]solver.ExecResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Exec != sorted[j].Exec {
			return sorted[i].Exec > sorted[j].Exec
		}
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

func printSummary(w io.Writer, results []solver.ExecResult, warnings []string) {
	total, reachable, zero := countStats(results)

	fmt.Fprintf(w, "Pipeline Max-Exec Analysis\n")
	fmt.Fprintf(w, "==========================\n\n")
	fmt.Fprintf(w, "Total nodes: %d\n", total)
	fmt.Fprintf(w, "Reachable:   %d\n", reachable)
	fmt.Fprintf(w, "Zero-exec:   %d\n\n", zero)

	for _, warn := range warnings {
		fmt.Fprintf(w, "⚠  %s\n", warn)
	}
	if len(warnings) > 0 {
		fmt.Fprintln(w)
	}
}

func countStats(results []solver.ExecResult) (total, reachable, zero int) {
	for _, r := range results {
		total++
		if r.Reachable {
			reachable++
			if r.Exec == 0 {
				zero++
			}
		}
	}
	return
}

func printTable(w io.Writer, sorted []solver.ExecResult) {
	fmt.Fprintf(w, "%-55s %12s %12s %s\n", "Node", "MaxHit", "MaxExec", "Source")
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 95))
	for _, r := range sorted {
		fmt.Fprintf(w, "%-55s %12s %12s %s\n",
			truncate(r.Name, 55), formatMaxHit(r.MaxHit), formatExec(r.Exec), r.Source)
	}
}

func formatMaxHit(mh uint64) string {
	if mh >= math.MaxUint32 {
		return "UINT_MAX"
	}
	return fmt.Sprintf("%d", mh)
}

func formatExec(e int64) string {
	if e <= 0 {
		return "0"
	}
	return fmt.Sprintf("%d", e)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
