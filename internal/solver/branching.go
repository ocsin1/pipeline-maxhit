package solver

import (
	"slices"

	"github.com/ocsin1/pipeline-maxhit/internal/graph"
	"github.com/ocsin1/pipeline-maxhit/internal/pipeline"
)

// ResolveBranching finds branching Controllable children and re-solves
// flow for each isolated child, merging per-node maximums into results.
// Uses in-place edge modification (save/restore) to avoid graph cloning.
func ResolveBranching(g *graph.Graph, base []ExecResult) []ExecResult {
	branches := findBranchPoints(g)
	if len(branches) == 0 {
		return base
	}

	zeroSet := make(map[string]bool)
	for _, r := range base {
		if r.Reachable && r.Exec == 0 {
			zeroSet[r.Name] = true
		}
	}
	if len(zeroSet) == 0 {
		return base
	}

	// Sort branches: prioritize those with zero-exec children
	slices.SortFunc(branches, func(a, b branchPoint) int {
		aHasZero := hasAnyZero(a.children, zeroSet)
		bHasZero := hasAnyZero(b.children, zeroSet)
		if aHasZero != bHasZero {
			if aHasZero {
				return -1
			}
			return 1
		}
		return 0
	})

	const maxRuns = 1000
	var extra [][]ExecResult
	runCount := 0

	for _, bp := range branches {
		for _, child := range bp.children {
			if !zeroSet[child] {
				continue
			}
			if runCount >= maxRuns {
				goto done
			}
			runCount++

			// Save state, modify in-place, solve, restore.
			snap := saveState(g, bp.parent, bp.children, child)
			applyIsolation(g, bp.parent, child)
			fn := BuildFlowNetwork(g)
			fn.MaxFlow()
			extra = append(extra, ExtractResults(fn, g))
			restoreState(g, snap)
		}
	}
done:

	if len(extra) == 0 {
		return base
	}

	mergeResultsInto(base, extra)
	return base
}

// snapshot captures graph state before isolation so it can be restored.
type snapshot struct {
	parent      string
	outEdges    []graph.Edge              // original OutEdges[parent]
	inEdges     map[string][]graph.Edge    // original InEdges for each removed sibling
	reachable   map[string]bool            // original Reachable flags
	blockedBy   map[string]string          // original BlockedBy flags
}

func saveState(g *graph.Graph, parent string, allChildren []string, keep string) *snapshot {
	s := &snapshot{
		parent:    parent,
		outEdges:  g.OutEdges[parent],
		inEdges:   make(map[string][]graph.Edge),
		reachable: make(map[string]bool, len(g.Nodes)),
		blockedBy: make(map[string]string),
	}
	// Save InEdges of siblings that will be removed
	for _, c := range allChildren {
		if c == keep {
			continue
		}
		s.inEdges[c] = g.InEdges[c]
	}
	// Save reachability and blockedBy (only for nodes that are set)
	for name, info := range g.Nodes {
		if info.Reachable {
			s.reachable[name] = true
		}
		if info.BlockedBy != "" {
			s.blockedBy[name] = info.BlockedBy
		}
	}
	return s
}

func restoreState(g *graph.Graph, s *snapshot) {
	// Restore edges
	g.OutEdges[s.parent] = s.outEdges
	for child, edges := range s.inEdges {
		g.InEdges[child] = edges
	}
	// Restore reachability and blockedBy
	for name, info := range g.Nodes {
		info.Reachable = s.reachable[name]
		if b, ok := s.blockedBy[name]; ok {
			info.BlockedBy = b
		} else {
			info.BlockedBy = ""
		}
	}
}

// applyIsolation modifies g in-place: removes all normal sibling edges
// from parent except the one to `keep`, then recomputes reachability.
func applyIsolation(g *graph.Graph, parent, keep string) {
	edges := g.OutEdges[parent]
	kept := make([]graph.Edge, 0, len(edges))
	for _, e := range edges {
		if e.To == keep || !e.IsNormal() {
			kept = append(kept, e)
			continue
		}
		// Remove from InEdges of sibling
		inList := g.InEdges[e.To]
		for i, ie := range inList {
			if ie.From == parent && ie.Kind == e.Kind {
				g.InEdges[e.To] = append(inList[:i], inList[i+1:]...)
				break
			}
		}
	}
	g.OutEdges[parent] = kept

	g.RecomputeReachability()
	g.ReapplyBlockerPreprocessing()
	g.RecomputeReachability()
}

func hasAnyZero(children []string, zeroSet map[string]bool) bool {
	for _, c := range children {
		if zeroSet[c] {
			return true
		}
	}
	return false
}

type branchPoint struct {
	parent   string
	children []string
}

func findBranchPoints(g *graph.Graph) []branchPoint {
	var out []branchPoint
	for parent, edges := range g.OutEdges {
		var candidates []string
		for _, e := range edges {
			if !e.IsNormal() {
				continue
			}
			info := g.Nodes[e.To]
			if info == nil || !info.Pipeline.Enabled {
				continue
			}
			if info.Pipeline.RecogClass == pipeline.RecogInvisible {
				continue
			}
			candidates = append(candidates, e.To)
		}
		if len(candidates) > 1 {
			out = append(out, branchPoint{parent: parent, children: candidates})
		}
	}
	return out
}

func mergeResultsInto(base []ExecResult, batches [][]ExecResult) {
	byName := make(map[string]int, len(base))
	for i, r := range base {
		byName[r.Name] = i
	}
	for _, batch := range batches {
		for _, r := range batch {
			idx, ok := byName[r.Name]
			if ok && r.Exec > base[idx].Exec {
				base[idx] = r
			}
		}
	}
}
