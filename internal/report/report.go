package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ocsin1/pipeline-maxhit/internal/solver"
)

type TaskResult struct {
	Name    string
	Entry   string
	Results []solver.ExecResult
}

// PrintOptions controls output formatting.
type PrintOptions struct {
	ShowAllNodes bool // include unreachable nodes
	ShowSCC      bool // print SCC warnings
}

// Print writes formatted results for one or more tasks.
// reachableByTask maps task name → set of reachable node names (for SCC filtering).
func Print(w io.Writer, tasks []TaskResult, sccWarnings []string, reachableByTask map[string]map[string]bool, opts PrintOptions) {
	if opts.ShowSCC {
		printSCCWarnings(w, sccWarnings, reachableByTask, tasks)
	}

	for ti, tr := range tasks {
		if len(tasks) > 1 {
			fmt.Fprintf(w, "\n### %s (entry: %s)\n\n", tr.Name, tr.Entry)
		}
		printOneTask(w, tr, opts)
		if ti < len(tasks)-1 {
			fmt.Fprintln(w)
		}
	}
}

func printSCCWarnings(w io.Writer, warnings []string, reachableByTask map[string]map[string]bool, tasks []TaskResult) {
	allReachable := make(map[string]bool)
	for _, tr := range tasks {
		for _, r := range tr.Results {
			if r.Reachable {
				allReachable[r.Name] = true
			}
		}
	}

	var relevant []string
	for _, warn := range warnings {
		if sccOverlaps(warn, allReachable) {
			relevant = append(relevant, warn)
		}
	}
	if len(relevant) == 0 {
		return
	}
	for _, warn := range relevant {
		fmt.Fprintf(w, "⚠  %s\n", warn)
	}
	fmt.Fprintln(w)
}

func sccOverlaps(warn string, reachable map[string]bool) bool {
	// Warning format: "SCC with unbounded max_hit: NodeA, NodeB, ..."
	prefix := "SCC with unbounded max_hit: "
	idx := strings.Index(warn, prefix)
	if idx < 0 {
		return true // unexpected format — show it
	}
	nodes := warn[idx+len(prefix):]
	for _, name := range strings.Split(nodes, ", ") {
		name = strings.TrimSpace(name)
		if reachable[name] {
			return true
		}
	}
	return false
}

func printOneTask(w io.Writer, tr TaskResult, opts PrintOptions) {
	total, reachable, zero := countStats(tr.Results)

	fmt.Fprintf(w, "节点总数: %d  可达: %d  零执行: %d\n\n", total, reachable, zero)

	sorted := sortResults(tr.Results)
	printTable(w, sorted, opts)
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

func printTable(w io.Writer, sorted []solver.ExecResult, opts PrintOptions) {
	fmt.Fprintf(w, "%-55s %12s %s\n", "节点", "MaxExec", "来源")
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 80))

	printed := 0
	for _, r := range sorted {
		if !opts.ShowAllNodes && !r.Reachable {
			continue
		}
		fmt.Fprintf(w, "%-55s %12s %s\n",
			truncate(r.Name, 55), formatExec(r.Exec), r.Source)
		printed++
	}
	if printed == 0 {
		fmt.Fprintln(w, "(无可达节点)")
	}
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
