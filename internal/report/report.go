package report

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/ocsin1/pipeline-maxhit/internal/solver"
)

type TaskResult struct {
	Name    string
	Entry   string
	Results []solver.ExecResult
}

func Print(w io.Writer, tasks []TaskResult, sccWarnings []string) {
	printWarnings(w, sccWarnings)

	for ti, tr := range tasks {
		if len(tasks) > 1 {
			fmt.Fprintf(w, "\n### %s (entry: %s)\n\n", tr.Name, tr.Entry)
		}
		printOneTask(w, tr)
		if ti < len(tasks)-1 {
			fmt.Fprintln(w)
		}
	}
}

func printWarnings(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	for _, warn := range warnings {
		fmt.Fprintf(w, "⚠  %s\n", warn)
	}
	fmt.Fprintln(w)
}

func printOneTask(w io.Writer, tr TaskResult) {
	sorted := sortResults(tr.Results)
	total, reachable, zero := countStats(tr.Results)

	fmt.Fprintf(w, "节点总数: %d  可达: %d  零执行: %d\n\n", total, reachable, zero)
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
	fmt.Fprintf(w, "%-55s %12s %12s %s\n", "节点", "MaxHit", "MaxExec", "来源")
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
