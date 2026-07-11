package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// --- Task definition types ---

type TaskDef struct {
	Name             string          `json:"name"`
	Entry            string          `json:"entry"`
	Option           []string        `json:"option"`
	PipelineOverride json.RawMessage `json:"pipeline_override"`
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
	if err := json.Unmarshal(stripComments(data), &tf); err != nil {
		return nil, fmt.Errorf("parsing task file %s: %w", path, err)
	}
	return &tf, nil
}

// stripComments removes // line comments from JSON-like content.
// It respects string boundaries so // inside a string value is preserved.
func stripComments(data []byte) []byte {
	var out []byte
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		b := data[i]
		if escaped {
			out = append(out, b)
			escaped = false
			continue
		}
		if inString {
			out = append(out, b)
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}
		if b == '"' {
			inString = true
			out = append(out, b)
			continue
		}
		if b == '/' && i+1 < len(data) && data[i+1] == '/' {
			// Line comment — skip to end of line, keep the newline.
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
			continue
		}
		out = append(out, b)
	}
	return out
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
	Type             string          `json:"type"`
	DefaultCase      json.RawMessage `json:"default_case"`
	Cases            []CaseDef       `json:"cases"`
	PipelineOverride json.RawMessage `json:"pipeline_override"` // input/hotkey type
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

// MaxComboLimit is the maximum number of combinations to enumerate.
// When exceeded, fall back to union-only (optimistic upper bound).
const MaxComboLimit = 5000

// OverridePlan holds the analysis result for a task's options.
type OverridePlan struct {
	// StructModGroups: top-level option groups that modify graph structure.
	// Each group's cases may contain nested SubGroups (only active when the case is selected).
	StructModGroups []structModGroup
	// UnionCases: all cases from options that only modify enabled/max_hit/template/etc.
	UnionCases []CaseDef
}

// structModCase is a single case within a struct-mod option group.
// SubGroups are sub-options that only activate when this specific case is selected.
type structModCase struct {
	CaseDef
	SubGroups []structModGroup
}

type structModGroup struct {
	Type  string          // "switch", "select", "checkbox"
	Cases []structModCase // all cases in this group
}

// AnalyzeOptions builds an OverridePlan from the task definition.
func AnalyzeOptions(tf *TaskFile, taskName string) *OverridePlan {
	td := tf.FindTask(taskName)
	if td == nil {
		return nil
	}

	plan := &OverridePlan{}
	plan.StructModGroups = collectStructGroups(tf, td.Option, make(map[string]bool))
	return plan
}

// collectStructGroups returns struct-mod groups from the given option names.
// For non-struct options, their cases go into UnionCases and their sub-options
// are collected as independent groups. For struct-mod options, sub-options
// are nested under the parent case that references them.
func collectStructGroups(tf *TaskFile, names []string, visited map[string]bool) []structModGroup {
	var groups []structModGroup
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

		isStruct := hasStructMods(opt.Cases)

		if isStruct {
			// Build cases with nested sub-options.
			smCases := make([]structModCase, len(opt.Cases))
			for i, c := range opt.Cases {
				smCases[i] = structModCase{
					CaseDef:   c,
					SubGroups: collectStructGroups(tf, c.Option, visited),
				}
			}
			groups = append(groups, structModGroup{
				Type:  opt.Type,
				Cases: smCases,
			})
		} else {
			// Non-struct: cases are unioned; sub-options become independent groups.
			plan := &OverridePlan{} // dummy — we only use this path for recursion
			_ = plan
			for _, c := range opt.Cases {
				if len(c.Option) > 0 {
					subGroups := collectStructGroups(tf, c.Option, visited)
					groups = append(groups, subGroups...)
				}
			}
		}
	}
	return groups
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

// comboCount computes the number of combos without allocating them.
func (plan *OverridePlan) comboCount() int {
	if len(plan.StructModGroups) == 0 {
		return 1
	}
	return countGroupList(plan.StructModGroups, MaxComboLimit)
}

func countGroupList(groups []structModGroup, limit int) int {
	total := 1
	for _, g := range groups {
		n := countGroup(g)
		if n > limit/total {
			return limit + 1
		}
		total *= n
	}
	return total
}

func countGroup(g structModGroup) int {
	switch g.Type {
	case "checkbox":
		// 2^|cases| subsets, each selected case may have sub-groups.
		// We sum over all subsets: for each case c, either excluded (1) or included (sub[c]).
		n := 1
		for _, c := range g.Cases {
			if len(c.SubGroups) > 0 {
				n *= (1 + countGroupList(c.SubGroups, MaxComboLimit))
			} else {
				n *= 2
			}
			if n > MaxComboLimit {
				return MaxComboLimit + 1
			}
		}
		return n
	default:
		// switch/select: pick exactly one case.
		n := 0
		for _, c := range g.Cases {
			if len(c.SubGroups) > 0 {
				n += countGroupList(c.SubGroups, MaxComboLimit)
			} else {
				n++
			}
			if n > MaxComboLimit {
				return MaxComboLimit + 1
			}
		}
		return n
	}
}

// EnumerateCombos generates all valid case combinations.
// Returns nil if combos exceed MaxComboLimit (caller should fall back to union).
func (plan *OverridePlan) EnumerateCombos() []OverrideCombo {
	if len(plan.StructModGroups) == 0 {
		return []OverrideCombo{{}}
	}
	if plan.comboCount() > MaxComboLimit {
		return nil
	}
	var combos []OverrideCombo
	enumerateGroupList(plan.StructModGroups, nil, &combos, MaxComboLimit)
	return combos
}

// enumerateGroupList processes a list of groups sequentially.
func enumerateGroupList(groups []structModGroup, selected []CaseDef, combos *[]OverrideCombo, limit int) {
	if len(*combos) >= limit {
		return
	}
	if len(groups) == 0 {
		*combos = append(*combos, OverrideCombo{SelectedCases: slices.Clone(selected)})
		return
	}
	g := groups[0]
	rest := groups[1:]
	switch g.Type {
	case "checkbox":
		enumerateCheckboxGroup(g, 0, selected, rest, combos, limit)
	default:
		// switch/select: pick exactly one case.
		for _, c := range g.Cases {
			newSel := appendIfEffectful(selected, c.CaseDef)
			// Insert sub-groups before rest.
			next := append(append([]structModGroup{}, c.SubGroups...), rest...)
			enumerateGroupList(next, newSel, combos, limit)
		}
	}
}

// enumerateCheckboxGroup enumerates subsets of a checkbox group's cases.
func enumerateCheckboxGroup(g structModGroup, i int, selected []CaseDef, rest []structModGroup, combos *[]OverrideCombo, limit int) {
	if len(*combos) >= limit {
		return
	}
	if i >= len(g.Cases) {
		enumerateGroupList(rest, selected, combos, limit)
		return
	}
	c := g.Cases[i]
	// Exclude this case.
	enumerateCheckboxGroup(g, i+1, selected, rest, combos, limit)
	// Include this case: insert its sub-groups before rest.
	newSel := appendIfEffectful(selected, c.CaseDef)
	next := append(append([]structModGroup{}, c.SubGroups...), rest...)
	enumerateCheckboxGroup(g, i+1, newSel, next, combos, limit)
}

// appendIfEffectful appends c to selected only if it has a non-empty pipeline_override.
func appendIfEffectful(selected []CaseDef, c CaseDef) []CaseDef {
	if len(c.PipelineOverride) == 0 {
		return selected
	}
	return append(selected, c)
}

// --- Merge helpers ---

// ApplyOverridesUnion applies all cases in optimistic-union mode (used for non-struct options).
// Also applies task-level pipeline_override and input-type option-level overrides.
func (p *Pipeline) ApplyOverridesUnion(tf *TaskFile, taskName string) {
	td := tf.FindTask(taskName)
	if td == nil {
		return
	}
	// Task-level pipeline_override.
	if len(td.PipelineOverride) > 0 {
		mergeOverride(p, td.PipelineOverride)
	}
	// All option cases + input-type option-level overrides.
	allOverrides := collectAllOverrides(tf, td.Option)
	for _, raw := range allOverrides {
		mergeOverride(p, raw)
	}
}

// ApplyCombo applies a single combination of struct-modifying overrides.
func (p *Pipeline) ApplyCombo(combo OverrideCombo) {
	for _, c := range combo.SelectedCases {
		mergeOverride(p, c.PipelineOverride)
	}
}

// collectAllOverrides collects all pipeline_override values from the option tree
// (both case-level and input-type option-level), recursively walking sub-options.
func collectAllOverrides(tf *TaskFile, optionNames []string) []json.RawMessage {
	var result []json.RawMessage
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
			// Option-level override (input/hotkey types).
			if len(opt.PipelineOverride) > 0 {
				result = append(result, opt.PipelineOverride)
			}
			// Case-level overrides.
			for _, c := range opt.Cases {
				if len(c.PipelineOverride) > 0 {
					result = append(result, c.PipelineOverride)
				}
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
