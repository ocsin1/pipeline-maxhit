package graph

import (
	"github.com/ocsin1/pipeline-maxhit/internal/pipeline"
)

type Graph struct {
	Nodes    map[string]*NodeInfo
	InEdges  map[string][]Edge
	OutEdges map[string][]Edge
	Entry    string
}

type NodeInfo struct {
	Pipeline  *pipeline.NodeData
	Reachable bool
	BlockedBy string
}

type Edge struct {
	From   string
	To     string
	Kind   pipeline.EdgeKind
	Anchor bool
}

func Build(p *pipeline.Pipeline, entry string) *Graph {
	g := &Graph{
		Nodes:    make(map[string]*NodeInfo),
		InEdges:  make(map[string][]Edge),
		OutEdges: make(map[string][]Edge),
		Entry:    entry,
	}
	for name, nd := range p.Nodes {
		g.Nodes[name] = &NodeInfo{Pipeline: nd}
	}
	for name, nd := range p.Nodes {
		g.buildEdges(name, nd.Next, pipeline.EdgeNextNormal, pipeline.EdgeNextJumpBack)
		g.buildEdges(name, nd.OnError, pipeline.EdgeOnErrorNormal, pipeline.EdgeOnErrorJumpBack)
	}
	g.removeStopTaskEdges()
	g.applyBlockerPreprocessing()
	g.computeReachability()
	return g
}

func (g *Graph) buildEdges(from string, items []pipeline.NextItem, normalKind, jbKind pipeline.EdgeKind) {
	for _, item := range items {
		if g.shouldSkipEdge(item) {
			continue
		}
		kind := normalKind
		if item.JumpBack {
			kind = jbKind
		}
		edge := Edge{From: from, To: item.Name, Kind: kind, Anchor: item.Anchor}
		g.OutEdges[from] = append(g.OutEdges[from], edge)
		g.InEdges[item.Name] = append(g.InEdges[item.Name], edge)
	}
}

func (g *Graph) shouldSkipEdge(item pipeline.NextItem) bool {
	target, ok := g.Nodes[item.Name]
	return ok && target.Pipeline.RecogClass == pipeline.RecogInvisible
}

// --- StopTask handling ---

func (g *Graph) removeStopTaskEdges() {
	for name, info := range g.Nodes {
		if !info.Pipeline.IsStopTask() {
			continue
		}
		for _, e := range g.OutEdges[name] {
			g.removeEdge(name, e)
		}
	}
}

// --- Blocker preprocessing ---

const maxUint32 = uint64(^uint32(0))

func (g *Graph) applyBlockerPreprocessing() {
	for uName, uInfo := range g.Nodes {
		g.blockerPreprocessNode(uName, uInfo)
	}
}

func (g *Graph) blockerPreprocessNode(uName string, uInfo *NodeInfo) {
	nd := uInfo.Pipeline
	if nd == nil {
		return
	}
	ordered := g.buildOrderedNextEdges(uName, nd)
	if len(ordered) == 0 {
		return
	}
	blockerIdx := g.findFirstBlocker(ordered)
	if blockerIdx < 0 {
		return
	}
	g.removeBlockedEdges(uName, ordered, blockerIdx)
}

func (g *Graph) buildOrderedNextEdges(uName string, nd *pipeline.NodeData) []Edge {
	var ordered []Edge
	for _, item := range nd.Next {
		wantKind := pipeline.EdgeNextNormal
		if item.JumpBack {
			wantKind = pipeline.EdgeNextJumpBack
		}
		for _, e := range g.OutEdges[uName] {
			if e.To == item.Name && e.Kind == wantKind {
				ordered = append(ordered, e)
				break
			}
		}
	}
	return ordered
}

func (g *Graph) findFirstBlocker(edges []Edge) int {
	for i, e := range edges {
		target, ok := g.Nodes[e.To]
		if ok && target.Pipeline.IsBlocker() {
			return i
		}
	}
	return -1
}

func (g *Graph) removeBlockedEdges(uName string, ordered []Edge, blockerIdx int) {
	blockerEdge := ordered[blockerIdx]
	blockerNode := g.Nodes[blockerEdge.To]

	if !blockerEdge.IsJumpBack() {
		// Non-jb DirectHit: captures the ONE normal hit. ALL later nodes unreachable.
		for i := blockerIdx + 1; i < len(ordered); i++ {
			g.removeEdge(uName, ordered[i])
		}
		return
	}

	if blockerNode.Pipeline.MaxHitOrDefault() >= maxUint32 {
		// Jb DirectHit, unlimited: captures every jb-return. Later non-jb unreachable.
		for i := blockerIdx + 1; i < len(ordered); i++ {
			if !ordered[i].IsJumpBack() {
				g.removeEdge(uName, ordered[i])
			}
		}
	}
	// Jb DirectHit with finite max_hit: keep later edges (optimistic).
}

// --- Edge helpers ---

func (e Edge) IsJumpBack() bool {
	return e.Kind == pipeline.EdgeNextJumpBack || e.Kind == pipeline.EdgeOnErrorJumpBack
}

func (e Edge) IsNormal() bool {
	return e.Kind == pipeline.EdgeNextNormal || e.Kind == pipeline.EdgeOnErrorNormal
}

func (g *Graph) removeEdge(from string, edge Edge) {
	g.removeFromOutEdges(from, edge)
	g.removeFromInEdges(edge.To, from, edge.Kind)
}

func (g *Graph) removeFromOutEdges(from string, edge Edge) {
	out := g.OutEdges[from]
	for i, e := range out {
		if e.To == edge.To && e.Kind == edge.Kind {
			g.OutEdges[from] = append(out[:i], out[i+1:]...)
			return
		}
	}
}

func (g *Graph) removeFromInEdges(to, from string, kind pipeline.EdgeKind) {
	in := g.InEdges[to]
	for i, e := range in {
		if e.From == from && e.Kind == kind {
			g.InEdges[to] = append(in[:i], in[i+1:]...)
			return
		}
	}
}

// --- Reachability ---

func (g *Graph) RecomputeReachability() { g.computeReachability() }
func (g *Graph) ReapplyBlockerPreprocessing() { g.applyBlockerPreprocessing() }

func (g *Graph) computeReachability() {
	visited := make(map[string]bool)
	queue := []string{g.Entry}
	visited[g.Entry] = true

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.OutEdges[cur] {
			if visited[e.To] {
				continue
			}
			visited[e.To] = true
			queue = append(queue, e.To)
		}
	}

	for name, info := range g.Nodes {
		info.Reachable = visited[name]
	}
}
