package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// --- Task definition types ---

type TaskDef struct {
	Name   string   `json:"name"`
	Entry  string   `json:"entry"`
	Option []string `json:"option"`
}

type TaskFile struct {
	Tasks  []TaskDef                `json:"task"`
	Option map[string]json.RawMessage `json:"option"`
}

func ParseTaskFile(path string) (*TaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading task file: %w", err)
	}
	var tf TaskFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parsing task file %s: %w", path, err)
	}
	return &tf, nil
}

func (tf *TaskFile) FindTask(name string) *TaskDef {
	for i := range tf.Tasks {
		if tf.Tasks[i].Name == name {
			return &tf.Tasks[i]
		}
	}
	return nil
}

// --- Option/override types ---

type OptionDef struct {
	Type        string          `json:"type"`
	DefaultCase json.RawMessage `json:"default_case"`
	Cases       []CaseDef       `json:"cases"`
}

type CaseDef struct {
	Name             string          `json:"name"`
	PipelineOverride json.RawMessage `json:"pipeline_override"`
	Option           []string        `json:"option"`
}

// partialNode is a partial override for a single node.
type partialNode struct {
	Enabled     *bool           `json:"enabled"`
	MaxHit      *uint64         `json:"max_hit"`
	Inverse     *bool           `json:"inverse"`
	Recognition json.RawMessage `json:"recognition"`
	Action      json.RawMessage `json:"action"`
	Next        json.RawMessage `json:"next"`
	OnError     json.RawMessage `json:"on_error"`
}

// --- Override plan: classifies options and enumerates combos ---

// OverridePlan holds the analysis result for a task's options.
type OverridePlan struct {
	// StructModGroups: options that modify graph structure (next/recognition/action).
	// Each group is a list of case overrides; we select exactly one per switch/select group,
	// or any subset per checkbox group.
	StructModGroups []structModGroup
	// UnionCases: all cases from options that only modify enabled/max_hit/template/etc.
	UnionCases []CaseDef
}

type structModGroup struct {
	Type  string    // "switch", "select", "checkbox"
	Cases []CaseDef // all cases in this group
}

// AnalyzeOptions builds an OverridePlan from the task definition.
func AnalyzeOptions(tf *TaskFile, taskName string) *OverridePlan {
	td := tf.FindTask(taskName)
	if td == nil {
		return nil
	}

	plan := &OverridePlan{}
	collectOptions(tf, td.Option, plan, make(map[string]bool))
	return plan
}

func collectOptions(tf *TaskFile, names []string, plan *OverridePlan, visited map[string]bool) {
	for _, optName := range names {
		if visited[optName] {
			continue
		}
		visited[optName] = true

		raw, ok := tf.Option[optName]
		if !ok {
			continue
		}
		var opt OptionDef
		if err := json.Unmarshal(raw, &opt); err != nil {
			continue
		}
		if len(opt.Cases) == 0 {
			continue
		}

		if hasStructMods(opt.Cases) {
			plan.StructModGroups = append(plan.StructModGroups, structModGroup{
				Type:  opt.Type,
				Cases: opt.Cases,
			})
		} else {
			plan.UnionCases = append(plan.UnionCases, opt.Cases...)
		}

		for _, c := range opt.Cases {
			if len(c.Option) > 0 {
				collectOptions(tf, c.Option, plan, visited)
			}
		}
	}
}

func hasStructMods(cases []CaseDef) bool {
	for _, c := range cases {
		if rawHasStructMod(c.PipelineOverride) {
			return true
		}
	}
	return false
}

func rawHasStructMod(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var overrides map[string]json.RawMessage
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return false
	}
	for _, nodeOverride := range overrides {
		var pn partialNode
		if err := json.Unmarshal(nodeOverride, &pn); err != nil {
			continue
		}
		if len(pn.Next) > 0 || len(pn.OnError) > 0 || len(pn.Recognition) > 0 || len(pn.Action) > 0 {
			return true
		}
	}
	return false
}

// --- Combination enumeration ---

// OverrideCombo is one concrete selection of option cases.
type OverrideCombo struct {
	SelectedCases []CaseDef
}

// EnumerateCombos generates all valid case combinations from the OverridePlan.
func (plan *OverridePlan) EnumerateCombos() []OverrideCombo {
	if len(plan.StructModGroups) == 0 {
		return []OverrideCombo{{}} // single empty combo
	}

	var combos []OverrideCombo
	enumerateGroups(plan.StructModGroups, 0, nil, &combos)
	return combos
}

func enumerateGroups(groups []structModGroup, idx int, selected []CaseDef, combos *[]OverrideCombo) {
	if idx >= len(groups) {
		*combos = append(*combos, OverrideCombo{SelectedCases: slices.Clone(selected)})
		return
	}
	g := groups[idx]
	switch g.Type {
	case "checkbox":
		// Each case can be independently toggled → 2^n subsets.
		enumerateCheckbox(g.Cases, 0, selected, groups, idx, combos)
	default:
		// switch / select: pick exactly one case.
		for _, c := range g.Cases {
			enumerateGroups(groups, idx+1, append(selected, c), combos)
		}
	}
}

func enumerateCheckbox(cases []CaseDef, i int, selected []CaseDef, groups []structModGroup, idx int, combos *[]OverrideCombo) {
	if i >= len(cases) {
		enumerateGroups(groups, idx+1, selected, combos)
		return
	}
	// Exclude this case
	enumerateCheckbox(cases, i+1, selected, groups, idx, combos)
	// Include this case
	enumerateCheckbox(cases, i+1, append(selected, cases[i]), groups, idx, combos)
}

// --- Merge helpers ---

// ApplyOverridesUnion applies all cases in optimistic-union mode (used for non-struct options).
func (p *Pipeline) ApplyOverridesUnion(tf *TaskFile, taskName string) {
	td := tf.FindTask(taskName)
	if td == nil {
		return
	}
	allCases := collectAllCases(tf, td.Option)
	for _, c := range allCases {
		mergeOverride(p, c.PipelineOverride)
	}
}

// ApplyCombo applies a single combination of struct-modifying overrides.
func (p *Pipeline) ApplyCombo(combo OverrideCombo) {
	for _, c := range combo.SelectedCases {
		mergeOverride(p, c.PipelineOverride)
	}
}

func collectAllCases(tf *TaskFile, optionNames []string) []CaseDef {
	var result []CaseDef
	visited := make(map[string]bool)
	var walk func(names []string)
	walk = func(names []string) {
		for _, optName := range names {
			if visited[optName] {
				continue
			}
			visited[optName] = true
			raw, ok := tf.Option[optName]
			if !ok {
				continue
			}
			var opt OptionDef
			if err := json.Unmarshal(raw, &opt); err != nil {
				continue
			}
			result = append(result, opt.Cases...)
			for _, c := range opt.Cases {
				if len(c.Option) > 0 {
					walk(c.Option)
				}
			}
		}
	}
	walk(optionNames)
	return result
}

func mergeOverride(p *Pipeline, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var overrides map[string]partialNode
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return
	}
	for nodeName, partial := range overrides {
		p.mergePartialNode(nodeName, partial)
	}
}

func (p *Pipeline) mergePartialNode(name string, partial partialNode) {
	nd := p.Nodes[name]
	if nd == nil {
		return
	}
	// Enabled: OR.
	if partial.Enabled != nil && *partial.Enabled {
		nd.Enabled = true
	}
	// MaxHit: MAX.
	if partial.MaxHit != nil && *partial.MaxHit > nd.MaxHit {
		nd.MaxHit = *partial.MaxHit
	}
	// Recognition: prefer non-DirectHit (more controllable).
	if len(partial.Recognition) > 0 {
		if recogType, inverse, err := ParseRecogForTest(partial.Recognition); err == nil {
			if !isBlockerType(recogType, inverse) || nd.RecogType == "" {
				nd.RecogType = recogType
				nd.Inverse = inverse
				nd.RecogClass = ClassifyRecogForTest(nd)
			}
		}
	}
	// Action: override if non-default.
	if len(partial.Action) > 0 {
		if actType := parseActionType(partial.Action); actType != "DoNothing" {
			nd.ActionType = actType
		}
	}
	// Next / OnError: UNION.
	if len(partial.Next) > 0 {
		if extra, err := ParseNextForTest(partial.Next); err == nil {
			nd.Next = mergeNextLists(nd.Next, extra)
		}
	}
	if len(partial.OnError) > 0 {
		if extra, err := ParseNextForTest(partial.OnError); err == nil {
			nd.OnError = mergeNextLists(nd.OnError, extra)
		}
	}
}

func isBlockerType(recogType string, inverse bool) bool {
	return !inverse && recogType == "DirectHit"
}

func mergeNextLists(base, extra []NextItem) []NextItem {
	seen := make(map[string]bool)
	for _, item := range base {
		seen[itemKey(item)] = true
	}
	for _, item := range extra {
		key := itemKey(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		base = append(base, item)
	}
	return base
}

func itemKey(item NextItem) string {
	prefix := ""
	if item.JumpBack {
		prefix += "[JumpBack]"
	}
	if item.Anchor {
		prefix += "[Anchor]"
	}
	return prefix + item.Name
}
