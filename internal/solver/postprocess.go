package solver

import "github.com/ocsin1/pipeline-maxhit/internal/graph"

type ExecResult struct {
	Name      string
	MaxHit    uint64
	Exec      int64
	Reachable bool
	Source    string
}

func ExtractResults(fn *FlowNetwork, g *graph.Graph) []ExecResult {
	results := make([]ExecResult, 0, len(g.Nodes))
	for name, info := range g.Nodes {
		results = append(results, buildResult(name, info, fn, g))
	}
	return results
}

func buildResult(name string, info *graph.NodeInfo, fn *FlowNetwork, g *graph.Graph) ExecResult {
	r := ExecResult{
		Name:      name,
		MaxHit:    info.Pipeline.MaxHitOrDefault(),
		Reachable: info.Reachable,
	}
	if !info.Reachable {
		r.Source = "Unreachable"
		return r
	}
	if info.BlockedBy != "" {
		r.Source = "Blocked(by " + info.BlockedBy + ")"
		return r
	}
	r.Exec = fn.FlowOnEdge(nodeIn(name), nodeOut(name))
	r.Source = classifySource(name, g, fn)
	return r
}

func classifySource(name string, g *graph.Graph, fn *FlowNetwork) string {
	if name == g.Entry {
		return "Entry"
	}
	jbFlow := fn.FlowOnEdge("__SOURCE__", nodeIn(name))
	normalFlow := sumNormalInFlow(name, g, fn)

	switch {
	case jbFlow > 0 && normalFlow > 0:
		return "Mixed(JumpBack+Normal)"
	case jbFlow > 0:
		return "JumpBack"
	case normalFlow > 0:
		return firstNormalSource(name, g, fn)
	default:
		return "Zero"
	}
}

func sumNormalInFlow(name string, g *graph.Graph, fn *FlowNetwork) int64 {
	var total int64
	for _, e := range g.InEdges[name] {
		if !e.IsNormal() || !g.Nodes[e.From].Reachable {
			continue
		}
		total += fn.FlowOnEdge(nodeOut(e.From), nodeIn(name))
	}
	return total
}

func firstNormalSource(name string, g *graph.Graph, fn *FlowNetwork) string {
	for _, e := range g.InEdges[name] {
		if !e.IsNormal() || !g.Nodes[e.From].Reachable {
			continue
		}
		if fn.FlowOnEdge(nodeOut(e.From), nodeIn(name)) > 0 {
			return "Normal(from " + e.From + ")"
		}
	}
	return "Normal"
}
