package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ocsin1/pipeline-maxhit/internal/graph"
	"github.com/ocsin1/pipeline-maxhit/internal/pipeline"
	"github.com/ocsin1/pipeline-maxhit/internal/solver"
)

// buildTestPipeline creates an in-memory pipeline from a JSON string.
func buildTestPipeline(t *testing.T, jsonStr string) *pipeline.Pipeline {
	t.Helper()
	var rawNodes map[string]struct {
		Enabled    *bool              `json:"enabled"`
		MaxHit     *uint64            `json:"max_hit"`
		Recognition json.RawMessage   `json:"recognition"`
		Action     json.RawMessage    `json:"action"`
		Next       json.RawMessage    `json:"next"`
		OnError    json.RawMessage    `json:"on_error"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawNodes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	p := pipeline.DefaultPipeline()
	for name, raw := range rawNodes {
		rawRecog, _ := json.Marshal(raw.Recognition)
		rawNext, _ := json.Marshal(raw.Next)
		rawOnErr, _ := json.Marshal(raw.OnError)

		// Re-parse through the pipeline parser
		recogType, inverse, _ := pipeline.ParseRecogForTest(rawRecog)
		nextItems, _ := pipeline.ParseNextForTest(rawNext)
		onErrItems, _ := pipeline.ParseNextForTest(rawOnErr)

		nd := &pipeline.NodeData{
			Name:    name,
			Enabled: true,
			MaxHit:  ^uint64(0) >> 1, // default
		}
		if raw.Enabled != nil {
			nd.Enabled = *raw.Enabled
		}
		if raw.MaxHit != nil {
			nd.MaxHit = *raw.MaxHit
		}
		nd.RecogType = recogType
		nd.Inverse = inverse
		nd.Next = nextItems
		nd.OnError = onErrItems
		nd.RecogClass = pipeline.ClassifyRecogForTest(nd)

		p.Nodes[name] = nd
	}
	return &p
}

func TestLinearChain(t *testing.T) {
	jsonStr := `{
		"A": {"next": "B"},
		"B": {"next": "C"},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	checkExec(t, results, "B", 1)
	checkExec(t, results, "C", 1)
}

func TestJumpBack(t *testing.T) {
	jsonStr := `{
		"A": {"next": ["B", "[JumpBack]C"]},
		"B": {},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	total := fn.MaxFlow()
	t.Logf("Total flow: %d", total)

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	// B is a normal edge from A; C is jb and gets its own supply (capped at MAX_CAP)
	checkExec(t, results, "C", 1_000_000_000)
	// B gets the one normal hit from A
	checkExec(t, results, "B", 1)
}

func TestDirectHitBlocker(t *testing.T) {
	jsonStr := `{
		"A": {"next": ["B", "C"]},
		"B": {"recognition": "DirectHit"},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	// B is DirectHit non-jb → blocks C
	checkExec(t, results, "B", 1)
	checkExec(t, results, "C", 0)
}

func TestMaxHitLimit(t *testing.T) {
	jsonStr := `{
		"A": {"next": "B"},
		"B": {"max_hit": 3, "next": "C"},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	checkExec(t, results, "B", 1) // B gets at most 1 from A (linear chain)
	checkExec(t, results, "C", 1)
}

func checkExec(t *testing.T, results []solver.ExecResult, name string, expected int64) {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			if r.Exec != expected {
				t.Errorf("Node %s: expected exec=%d, got exec=%d (source=%s)", name, expected, r.Exec, r.Source)
			}
			return
		}
	}
	t.Errorf("Node %s not found in results", name)
}

// Ensure test-only helpers exist (add to pipeline package)
func init() {
	// Create temp dir for test
	os.MkdirAll(filepath.Join(os.TempDir(), "pipeline-maxhit-test"), 0755)
}
