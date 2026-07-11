package graph

import (
	"github.com/ocsin1/pipeline-maxhit/internal/pipeline"
)

// AnchorInfo holds the points-to analysis for anchor references.
type AnchorInfo struct {
	// Setters maps anchor name → list of node names that set this anchor.
	Setters map[string][]string
	// Clears maps anchor name → list of node names that clear this anchor (set to "").
	Clears map[string][]string
}

// CollectAnchors scans all nodes for anchor set/clear operations.
func CollectAnchors(g *Graph) *AnchorInfo {
	ai := &AnchorInfo{
		Setters: make(map[string][]string),
		Clears:  make(map[string][]string),
	}

	for name, info := range g.Nodes {
		for anchorName, target := range info.Pipeline.Anchors {
			if target == "" {
				ai.Clears[anchorName] = append(ai.Clears[anchorName], name)
			} else {
				ai.Setters[anchorName] = append(ai.Setters[anchorName], target)
			}
		}
	}
	return ai
}

// ResolveAnchors expands all anchor references into concrete edges.
//
// For each anchor reference u→[@X] in u's next/on_error list:
//   - Find all nodes in S[@X] (the points-to set)
//   - For each target s ∈ S[@X], add edge u→s with the same kind as the original reference
//
// Flow-insensitive: we add edges to ALL possible targets regardless of execution order.
// This is a safe over-approximation for the maximum-execution problem.
func ResolveAnchors(g *Graph) {
	ai := CollectAnchors(g)

	// Collect all anchor-reference edges that need expansion.
	type anchorEdge struct {
		from     string
		anchorName string
		kind     pipeline.EdgeKind
	}

	var toExpand []anchorEdge

	for uName, outEdges := range g.OutEdges {
		for _, e := range outEdges {
			if e.Anchor {
				toExpand = append(toExpand, anchorEdge{
					from:       uName,
					anchorName: e.To, // e.To is the anchor name (e.g. "@MyAnchor")
					kind:       e.Kind,
				})
			}
		}
	}

	// Remove original anchor-reference edges (they point to anchor names, not real nodes).
	for _, ae := range toExpand {
		g.removeAnchorEdge(ae.from, ae.anchorName, ae.kind)
	}

	// Expand each anchor reference to all possible targets.
	for _, ae := range toExpand {
		targets := ai.Setters[ae.anchorName]
		if len(targets) == 0 {
			// Anchor has no setters → the reference is unresolvable.
			// In the C++ code, get_anchor returns nullopt → the node is invisible.
			// For max-exec: this reference creates no edges.
			continue
		}

		for _, target := range targets {
			// Skip self-references and non-existent nodes.
			if targetNode, ok := g.Nodes[target]; !ok {
				continue
			} else if targetNode.Pipeline.RecogClass == pipeline.RecogInvisible {
				continue
			}

			edge := Edge{
				From:   ae.from,
				To:     target,
				Kind:   ae.kind,
				Anchor: true, // mark as anchor-expanded
			}
			g.OutEdges[ae.from] = append(g.OutEdges[ae.from], edge)
			g.InEdges[target] = append(g.InEdges[target], edge)
		}
	}
}

func (g *Graph) removeAnchorEdge(from, anchorName string, kind pipeline.EdgeKind) {
	out := g.OutEdges[from]
	for i, e := range out {
		if e.Anchor && e.To == anchorName && e.Kind == kind {
			g.OutEdges[from] = append(out[:i], out[i+1:]...)

			// Also remove from InEdges
			in := g.InEdges[anchorName]
			for j, ie := range in {
				if ie.From == from && ie.Kind == kind && ie.Anchor {
					g.InEdges[anchorName] = append(in[:j], in[j+1:]...)
					break
				}
			}
			return
		}
	}
}
