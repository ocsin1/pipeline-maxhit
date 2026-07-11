package solver

import (
	"math"

	"github.com/ocsin1/pipeline-maxhit/internal/graph"
)

const MAX_CAP = 1_000_000_000 // used in place of UINT32_MAX for flow network capacities

type FlowNetwork struct {
	Edges    []FlowEdge
	Adj      [][]int
	NodeToID map[string]int
	IDToNode map[int]string
	SinkID   int
	SourceID int
}

type FlowEdge struct {
	To  int
	Rev int
	Cap int64
}

func NewFlowNetwork() *FlowNetwork {
	fn := &FlowNetwork{
		NodeToID: make(map[string]int),
		IDToNode: make(map[int]string),
	}
	fn.SourceID = fn.addNode("__SOURCE__")
	fn.SinkID = fn.addNode("__SINK__")
	return fn
}

func (fn *FlowNetwork) addNode(name string) int {
	id := len(fn.Adj)
	fn.Adj = append(fn.Adj, nil)
	fn.NodeToID[name] = id
	fn.IDToNode[id] = name
	return id
}

func (fn *FlowNetwork) getOrCreate(name string) int {
	if id, ok := fn.NodeToID[name]; ok {
		return id
	}
	return fn.addNode(name)
}

func (fn *FlowNetwork) AddEdge(from, to string, cap int64) {
	u := fn.getOrCreate(from)
	v := fn.getOrCreate(to)
	fn.Edges = append(fn.Edges, FlowEdge{To: v, Rev: len(fn.Edges) + 1, Cap: cap})
	fn.Edges = append(fn.Edges, FlowEdge{To: u, Rev: len(fn.Edges) - 1, Cap: 0})
	fn.Adj[u] = append(fn.Adj[u], len(fn.Edges)-2)
	fn.Adj[v] = append(fn.Adj[v], len(fn.Edges)-1)
}

func nodeIn(name string) string  { return name + "_in" }
func nodeOut(name string) string { return name + "_out" }

func toCap(maxHit uint64) int64 {
	cap := int64(maxHit)
	if cap > MAX_CAP || cap <= 0 {
		return MAX_CAP
	}
	return cap
}

// BuildFlowNetwork constructs the max-flow network:
//   - Node splitting: v_in -> v_out, capacity = max_hit[v]
//   - Source -> entry_in: capacity 1 (bootstrap)
//   - Source -> v_in: capacity max_hit[v] for jb-reachable nodes (free budget)
//   - Normal edges p_out -> v_in: INF capacity
//   - Sink edges for leaves and jb-supplied non-leaves
func BuildFlowNetwork(g *graph.Graph) *FlowNetwork {
	fn := NewFlowNetwork()

	reachable := collectReachable(g)
	addNodeSplits(fn, g, reachable)
	addEntryEdge(fn, g, reachable)
	addNormalEdges(fn, g, reachable)
	jbReachable := computeJBReachable(g, reachable)
	addJBSupplyEdges(fn, g, jbReachable)
	addSinkEdges(fn, g, reachable, jbReachable)

	return fn
}

func collectReachable(g *graph.Graph) map[string]bool {
	reachable := make(map[string]bool)
	for name, info := range g.Nodes {
		if info.Reachable {
			reachable[name] = true
		}
	}
	return reachable
}

func addNodeSplits(fn *FlowNetwork, g *graph.Graph, reachable map[string]bool) {
	for name := range reachable {
		fn.AddEdge(nodeIn(name), nodeOut(name), toCap(g.Nodes[name].Pipeline.MaxHitOrDefault()))
	}
}

func addEntryEdge(fn *FlowNetwork, g *graph.Graph, reachable map[string]bool) {
	if !reachable[g.Entry] {
		return
	}
	fn.AddEdge("__SOURCE__", nodeIn(g.Entry), 1)
}

func addNormalEdges(fn *FlowNetwork, g *graph.Graph, reachable map[string]bool) {
	for _, edges := range g.OutEdges {
		for _, e := range edges {
			if !e.IsNormal() || !reachable[e.From] || !reachable[e.To] {
				continue
			}
			fn.AddEdge(nodeOut(e.From), nodeIn(e.To), MAX_CAP)
		}
	}
}

func computeJBReachable(g *graph.Graph, reachable map[string]bool) map[string]bool {
	jb := make(map[string]bool)
	var queue []string
	jb[g.Entry] = true
	queue = append(queue, g.Entry)

	// Seed: direct jb edges from reachable nodes.
	for _, edges := range g.OutEdges {
		for _, e := range edges {
			if !e.IsJumpBack() || !reachable[e.From] || !reachable[e.To] || jb[e.To] {
				continue
			}
			jb[e.To] = true
			queue = append(queue, e.To)
		}
	}

	// BFS through jb edges (jb chain propagation).
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.OutEdges[cur] {
			if !e.IsJumpBack() || !reachable[e.To] || jb[e.To] {
				continue
			}
			jb[e.To] = true
			queue = append(queue, e.To)
		}
	}
	return jb
}

func addJBSupplyEdges(fn *FlowNetwork, g *graph.Graph, jbReachable map[string]bool) {
	for name := range jbReachable {
		if name == g.Entry {
			continue // entry already has S edge
		}
		fn.AddEdge("__SOURCE__", nodeIn(name), toCap(g.Nodes[name].Pipeline.MaxHitOrDefault()))
	}
}

func addSinkEdges(fn *FlowNetwork, g *graph.Graph, reachable, jbReachable map[string]bool) {
	for name := range reachable {
		if !needsSinkEdge(g, name, reachable, jbReachable) {
			continue
		}
		fn.AddEdge(nodeOut(name), "__SINK__", MAX_CAP)
	}
}

func needsSinkEdge(g *graph.Graph, name string, reachable, jbReachable map[string]bool) bool {
	hasNormalOut := hasNormalOutgoing(g, name, reachable)
	if !hasNormalOut {
		return true // leaf always gets T edge
	}
	// Non-leaf jb-supplied nodes (except entry) need T edge for overflow.
	return jbReachable[name] && name != g.Entry
}

func hasNormalOutgoing(g *graph.Graph, name string, reachable map[string]bool) bool {
	for _, e := range g.OutEdges[name] {
		if e.IsNormal() && reachable[e.To] {
			return true
		}
	}
	return false
}

// SCC analysis below (diagnostic only).

type SCCResult struct {
	Components [][]string
	Warnings   []string
}

func FindSCCs(g *graph.Graph) *SCCResult {
	adj := buildNormalAdj(g)
	tracker := newTarjan(adj)
	for name := range g.Nodes {
		tracker.visit(name)
	}
	return tracker.result(g)
}

type tarjanState struct {
	adj     map[string][]string
	index   int
	indices map[string]int
	lowlink map[string]int
	onStack map[string]bool
	stack   []string
	sccs    [][]string
}

func newTarjan(adj map[string][]string) *tarjanState {
	return &tarjanState{
		adj:     adj,
		indices: make(map[string]int),
		lowlink: make(map[string]int),
		onStack: make(map[string]bool),
	}
}

func (t *tarjanState) visit(v string) {
	if _, done := t.indices[v]; done {
		return
	}
	t.strongconnect(v)
}

func (t *tarjanState) strongconnect(v string) {
	t.indices[v] = t.index
	t.lowlink[v] = t.index
	t.index++
	t.stack = append(t.stack, v)
	t.onStack[v] = true

	for _, w := range t.adj[v] {
		if _, ok := t.indices[w]; !ok {
			t.strongconnect(w)
			t.lowlink[v] = min(t.lowlink[v], t.lowlink[w])
		} else if t.onStack[w] {
			t.lowlink[v] = min(t.lowlink[v], t.indices[w])
		}
	}

	if t.lowlink[v] != t.indices[v] {
		return
	}
	var comp []string
	for {
		w := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.onStack[w] = false
		comp = append(comp, w)
		if w == v {
			break
		}
	}
	if len(comp) > 1 {
		t.sccs = append(t.sccs, comp)
	}
}

func (t *tarjanState) result(g *graph.Graph) *SCCResult {
	const maxU32 = uint64(math.MaxUint32)
	var warnings []string
	for _, comp := range t.sccs {
		if allUnbounded(comp, g, maxU32) {
			warnings = append(warnings, "SCC with unbounded max_hit: "+joinNames(comp))
		}
	}
	return &SCCResult{Components: t.sccs, Warnings: warnings}
}

func allUnbounded(comp []string, g *graph.Graph, limit uint64) bool {
	for _, name := range comp {
		if info, ok := g.Nodes[name]; !ok || info.Pipeline.MaxHitOrDefault() < limit {
			return false
		}
	}
	return true
}

func buildNormalAdj(g *graph.Graph) map[string][]string {
	adj := make(map[string][]string)
	for name := range g.Nodes {
		for _, e := range g.OutEdges[name] {
			if e.IsNormal() {
				adj[name] = append(adj[name], e.To)
			}
		}
	}
	return adj
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	s := names[0]
	for _, n := range names[1:] {
		s += ", " + n
	}
	return s
}
