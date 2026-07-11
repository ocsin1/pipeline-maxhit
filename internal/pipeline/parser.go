package pipeline

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// ParseNextItem parses a raw next-list entry which can be:
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
//   - "[JumpBack]NodeName"
//   - "[Anchor]AnchorName"
//   - "[JumpBack][Anchor]Name"
//   - "NodeName"
func ParseNextItem(raw string) NextItem {
	s := raw
	var jb, anchor bool
	for strings.HasPrefix(s, "[") {
		end := strings.IndexByte(s, ']')
		if end < 0 {
			break
		}
		attr := s[:end+1]
		switch attr {
		case "[JumpBack]":
			jb = true
		case "[Anchor]":
			anchor = true
		}
		s = s[end+1:]
	}
	return NextItem{Name: s, JumpBack: jb, Anchor: anchor}
}

// --- raw JSON structures for deserialization ---

// rawNode is the JSON representation of a pipeline node.
// We use json.RawMessage for fields with multiple possible shapes.
type rawNode struct {
	Enabled    *bool              `json:"enabled"`
	MaxHit     *uint64            `json:"max_hit"`
	Inverse    *bool              `json:"inverse"`
	Recognition json.RawMessage   `json:"recognition"` // string or object
	Action     json.RawMessage    `json:"action"`      // string or object
	Next       json.RawMessage    `json:"next"`        // string, object, or array
	OnError    json.RawMessage    `json:"on_error"`
	Anchor     map[string]string  `json:"anchor"`
}

// parseNextList handles next/on_error fields: single string, object, or array.
func parseNextList(raw json.RawMessage) ([]NextItem, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Try as string first
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []NextItem{ParseNextItem(single)}, nil
	}

	// Try as object (NodeAttr form)
	var obj struct {
		Name     string `json:"name"`
		JumpBack *bool  `json:"jump_back"`
		Anchor   *bool  `json:"anchor"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Name != "" {
		item := NextItem{Name: obj.Name}
		if obj.JumpBack != nil {
			item.JumpBack = *obj.JumpBack
		}
		if obj.Anchor != nil {
			item.Anchor = *obj.Anchor
		}
		return []NextItem{item}, nil
	}

	// Try as array (mixed string + object)
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("next/on_error: expected string, object, or array: %w", err)
	}

	var items []NextItem
	for _, elem := range arr {
		// Try string
		var s string
		if err := json.Unmarshal(elem, &s); err == nil {
			items = append(items, ParseNextItem(s))
			continue
		}
		// Try object
		var obj2 struct {
			Name     string `json:"name"`
			JumpBack *bool  `json:"jump_back"`
			Anchor   *bool  `json:"anchor"`
		}
		if err := json.Unmarshal(elem, &obj2); err == nil && obj2.Name != "" {
			item := NextItem{Name: obj2.Name}
			if obj2.JumpBack != nil {
				item.JumpBack = *obj2.JumpBack
			}
			if obj2.Anchor != nil {
				item.Anchor = *obj2.Anchor
			}
			items = append(items, item)
			continue
		}
		return nil, fmt.Errorf("unrecognised next/on_error element: %s", string(elem))
	}
	return items, nil
}

// parseRecognition extracts the recognition type string from a V1 or V2 spec.
// V1: "DirectHit"
// V2: {"type": "DirectHit", "param": {...}}
func parseRecognition(raw json.RawMessage) (recogType string, inverse bool, err error) {
	if len(raw) == 0 {
		return "DirectHit", false, nil // default
	}

	// V1: plain string
	var v1 string
	if err := json.Unmarshal(raw, &v1); err == nil {
		return v1, false, nil
	}

	// V2: object with "type" field
	var v2 struct {
		Type    string `json:"type"`
		Inverse *bool  `json:"inverse"`
	}
	if err := json.Unmarshal(raw, &v2); err == nil && v2.Type != "" {
		inv := false
		if v2.Inverse != nil {
			inv = *v2.Inverse
		}
		return v2.Type, inv, nil
	}

	return "", false, fmt.Errorf("unrecognised recognition format: %s", string(raw))
}

// parseNode converts a raw JSON node into NodeData.
func parseNode(name string, raw rawNode, defaults *NodeData) (*NodeData, error) {
	n := &NodeData{
		Name:    name,
		Enabled: defaults.Enabled,
		MaxHit:  defaults.MaxHit,
	}

	if raw.Enabled != nil {
		n.Enabled = *raw.Enabled
	}
	if raw.MaxHit != nil {
		n.MaxHit = *raw.MaxHit
	} else {
		n.MaxHit = defaults.MaxHit
	}
	if n.MaxHit == 0 {
		n.MaxHit = math.MaxUint32
	}

	// Parse recognition
	recogType, inverse, err := parseRecognition(raw.Recognition)
	if err != nil {
		return nil, fmt.Errorf("node %s: %w", name, err)
	}
	n.RecogType = recogType

	// Check for inverse in the main node fields too (overrides recognition-level inverse)
	if raw.Inverse != nil {
		inverse = *raw.Inverse
	}
	n.Inverse = inverse

	// Classify recognition
	n.RecogClass = classifyRecog(n)

	// Parse action type
	n.ActionType = parseActionType(raw.Action)

	// Parse next / on_error
	next, err := parseNextList(raw.Next)
	if err != nil {
		return nil, fmt.Errorf("node %s next: %w", name, err)
	}
	n.Next = next

	onErr, err := parseNextList(raw.OnError)
	if err != nil {
		return nil, fmt.Errorf("node %s on_error: %w", name, err)
	}
	n.OnError = onErr

	// Anchors
	n.Anchors = raw.Anchor

	return n, nil
}

// parseActionType extracts the action type string from a V1 or V2 spec.
// V1: "Click"
// V2: {"type": "Click", "param": {...}}
func parseActionType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "DoNothing" // default
	}

	// V1: plain string
	var v1 string
	if err := json.Unmarshal(raw, &v1); err == nil {
		return v1
	}

	// V2: object with "type" field
	var v2 struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &v2); err == nil && v2.Type != "" {
		return v2.Type
	}

	return "DoNothing"
}

func classifyRecog(n *NodeData) RecogClass {
	if n.Inverse {
		return RecogInvisible
	}
	if n.RecogType == "DirectHit" {
		return RecogBlocker
	}
	return RecogControllable
}

// ParsePipeline reads all pipeline JSON files from a directory and returns a merged Pipeline.
func ParsePipeline(pipelineDir string, defaultsPath string) (*Pipeline, error) {
	p := DefaultPipeline()

	// Load defaults
	if defaultsPath != "" {
		if err := p.loadDefaults(defaultsPath); err != nil {
			return nil, fmt.Errorf("loading defaults: %w", err)
		}
	}

	// Walk pipeline directory
	err := filepath.Walk(pipelineDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip hidden / internal directories
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		var rawNodes map[string]rawNode
		if err := json.Unmarshal(stripComments(data), &rawNodes); err != nil {
			return nil
		}

		for name, raw := range rawNodes {
			// Skip $ prefixed nodes (reserved / internal)
			if strings.HasPrefix(name, "$") {
				continue
			}

			node, err := parseNode(name, raw, &p.Defaults)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			// Later files override earlier ones (consistent with C++ PipelineResMgr)
			p.Nodes[name] = node
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// ParseRecogForTest is a test helper that extracts recognition type from raw JSON.
func ParseRecogForTest(raw json.RawMessage) (recogType string, inverse bool, err error) {
	return parseRecognition(raw)
}

// ParseNextForTest is a test helper that parses a next-list.
func ParseNextForTest(raw json.RawMessage) ([]NextItem, error) {
	return parseNextList(raw)
}

// ParseActionTypeForTest is a test helper that extracts action type string.
func ParseActionTypeForTest(raw json.RawMessage) string {
	return parseActionType(raw)
}

// ClassifyRecogForTest is a test helper.
func ClassifyRecogForTest(n *NodeData) RecogClass {
	return classifyRecog(n)
}

func (p *Pipeline) loadDefaults(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Look for "Default" entry
	if defRaw, ok := raw["Default"]; ok {
		var defNode rawNode
		if err := json.Unmarshal(defRaw, &defNode); err != nil {
			return err
		}
		defaults := &p.Defaults
		if defNode.Enabled != nil {
			defaults.Enabled = *defNode.Enabled
		}
		if defNode.MaxHit != nil {
			defaults.MaxHit = *defNode.MaxHit
		}
	}

	return nil
}
