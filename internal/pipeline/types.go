package pipeline

import "math"

// RecogClass classifies how a node's recognition behaves in a next-list scan.
type RecogClass int

const (
	RecogBlocker     RecogClass = iota // DirectHit without inverse - always hits
	RecogControllable                   // other types - recognition can be made to fail
	RecogInvisible                      // inverse DirectHit - never hits, effectively absent
)

// NextItem is a single entry in a node's "next" or "on_error" list.
type NextItem struct {
	Name     string // target node name, or anchor name when Anchor is true
	JumpBack bool
	Anchor   bool // true -> Name is an anchor reference, resolved at runtime
}

// NodeData is the parsed representation of a pipeline node.
type NodeData struct {
	Name       string
	Enabled    bool
	MaxHit     uint64 // default: math.MaxUint32
	RecogType  string // e.g. "DirectHit", "TemplateMatch", "OCR", ...
	RecogClass RecogClass
	Inverse    bool
	ActionType string // "DoNothing", "Click", "StopTask", "Custom", ...
	Next       []NextItem
	OnError    []NextItem
	Anchors    map[string]string // anchor name -> target node ("" means clear)
}

// MaxHitOrDefault returns the effective max_hit value.
func (n *NodeData) MaxHitOrDefault() uint64 {
	if n.MaxHit == 0 {
		return math.MaxUint32
	}
	return n.MaxHit
}

// IsBlocker returns true when this node acts as a blocker in next-list scans.
func (n *NodeData) IsBlocker() bool {
	return n.RecogClass == RecogBlocker && n.Enabled && n.MaxHitOrDefault() > 0
}

// IsStopTask returns true if this node's action stops the entire task.
func (n *NodeData) IsStopTask() bool {
	return n.ActionType == "StopTask" || n.ActionType == "Stop"
}

// EdgeKind classifies the semantic kind of a directed edge.
type EdgeKind int

const (
	EdgeNextNormal EdgeKind = iota
	EdgeNextJumpBack
	EdgeOnErrorNormal
	EdgeOnErrorJumpBack
	EdgeAnchorRef // placeholder before resolution
)

// Edge is a directed connection between two nodes.
type Edge struct {
	From string
	To   string
	Kind EdgeKind
}

// Pipeline holds all parsed nodes.
type Pipeline struct {
	Nodes    map[string]*NodeData
	Defaults NodeData
}

// Default values for the pipeline.
func DefaultPipeline() Pipeline {
	return Pipeline{
		Nodes: make(map[string]*NodeData),
		Defaults: NodeData{
			Enabled:    true,
			MaxHit:     math.MaxUint32,
			RecogType:  "DirectHit",
			RecogClass: RecogBlocker,
			ActionType: "DoNothing",
		},
	}
}
