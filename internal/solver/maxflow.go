package solver

import "math"

const INF = math.MaxInt64 / 2

func (fn *FlowNetwork) MaxFlow() int64 {
	src, sink := fn.SourceID, fn.SinkID
	n := len(fn.Adj)
	var flow int64

	for {
		level := fn.bfsLevel(src, sink, n)
		if level[sink] < 0 {
			return flow
		}
		ptr := make([]int, n)
		for {
			pushed := fn.dfs(src, sink, INF, level, ptr)
			if pushed == 0 {
				break
			}
			flow += pushed
		}
	}
}

func (fn *FlowNetwork) bfsLevel(src, sink, n int) []int {
	level := make([]int, n)
	for i := range level {
		level[i] = -1
	}
	level[src] = 0
	queue := []int{src}

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, ei := range fn.Adj[u] {
			e := &fn.Edges[ei]
			if e.Cap <= 0 || level[e.To] >= 0 {
				continue
			}
			level[e.To] = level[u] + 1
			queue = append(queue, e.To)
		}
	}
	return level
}

func (fn *FlowNetwork) dfs(u, sink int, pushed int64, level, ptr []int) int64 {
	if u == sink {
		return pushed
	}
	for ptr[u] < len(fn.Adj[u]) {
		ei := fn.Adj[u][ptr[u]]
		e := &fn.Edges[ei]
		if e.Cap <= 0 || level[u]+1 != level[e.To] {
			ptr[u]++
			continue
		}
		tr := fn.dfs(e.To, sink, min64(pushed, e.Cap), level, ptr)
		if tr == 0 {
			ptr[u]++
			continue
		}
		e.Cap -= tr
		fn.Edges[e.Rev].Cap += tr
		return tr
	}
	return 0
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// FlowOnEdge returns the flow through the named edge (from reverse edge's residual).
func (fn *FlowNetwork) FlowOnEdge(from, to string) int64 {
	uid, uidOk := fn.NodeToID[from]
	vid, vidOk := fn.NodeToID[to]
	if !uidOk || !vidOk {
		return 0
	}
	for _, ei := range fn.Adj[uid] {
		e := &fn.Edges[ei]
		if e.To != vid {
			continue
		}
		return fn.Edges[e.Rev].Cap // reverse residual = forward flow
	}
	return 0
}
