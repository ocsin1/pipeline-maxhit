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
		Enabled    *bool            `json:"enabled"`
		MaxHit     *uint64          `json:"max_hit"`
		Recognition json.RawMessage `json:"recognition"`
		Action     json.RawMessage `json:"action"`
		Next       json.RawMessage `json:"next"`
		OnError    json.RawMessage `json:"on_error"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawNodes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	p := pipeline.DefaultPipeline()
	for name, raw := range rawNodes {
		rawRecog, _ := json.Marshal(raw.Recognition)
		rawNext, _ := json.Marshal(raw.Next)
		rawOnErr, _ := json.Marshal(raw.OnError)
		rawAction, _ := json.Marshal(raw.Action)

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
		nd.ActionType = pipeline.ParseActionTypeForTest(rawAction)
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

func TestDisabledNode(t *testing.T) {
	// FW line 298-301: disabled nodes are skipped in next list scan.
	jsonStr := `{
		"A": {"next": ["B", "C"]},
		"B": {"enabled": false},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	checkExec(t, results, "B", 0) // disabled → unreachable
	checkExec(t, results, "C", 1) // only C gets the hit
}

func TestOnErrorPath(t *testing.T) {
	// FW line 82-87: when recognition fails, fall back to on_error.
	// Both next and on_error are normal edges in our model.
	// With 1 unit from A, either B_or_C gets it (integral flow).
	jsonStr := `{
		"A": {"next": "B", "on_error": "C"},
		"B": {},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	// B and C share A's 1 unit; at least one gets 1.
	bExec := getExec(results, "B")
	cExec := getExec(results, "C")
	if bExec+cExec != 1 {
		t.Errorf("B+C exec should be 1, got B=%d C=%d", bExec, cExec)
	}
	// Both are reachable (edges exist)
	checkReachable(t, results, "B", true)
	checkReachable(t, results, "C", true)
}

func TestJumpBackFiniteLimit(t *testing.T) {
	// JB node with max_hit=1 should be limited, not get 1e9.
	jsonStr := `{
		"A": {"next": ["B", "[JumpBack]C"]},
		"B": {},
		"C": {"max_hit": 1}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	checkExec(t, results, "B", 1)
	checkExec(t, results, "C", 1) // max_hit=1 caps JB supply
}

func TestJumpBackChain(t *testing.T) {
	// JB chain: A → [JB]B → [JB]C. Both B and C should get JB supply.
	jsonStr := `{
		"A": {"next": "[JumpBack]B"},
		"B": {"next": "[JumpBack]C"},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	// B and C are jb-reachable → get JB supply (1e9 by default)
	checkExec(t, results, "B", solver.MAX_CAP)
	checkExec(t, results, "C", solver.MAX_CAP)
}

func TestStopTask(t *testing.T) {
	// FW: nodes with StopTask action terminate the chain.
	jsonStr := `{
		"A": {"next": "B"},
		"B": {"action": "StopTask", "next": "C"},
		"C": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	checkExec(t, results, "B", 1)
	checkExec(t, results, "C", 0) // B's StopTask removes edge to C
}

func TestEntryIsDirectHit(t *testing.T) {
	// Entry itself is DirectHit → still gets 1 from entry edge.
	jsonStr := `{
		"A": {"recognition": "DirectHit", "next": "B"},
		"B": {}
	}`
	p := buildTestPipeline(t, jsonStr)
	g := graph.Build(p, "A")
	fn := solver.BuildFlowNetwork(g)
	fn.MaxFlow()

	results := solver.ExtractResults(fn, g)
	checkExec(t, results, "A", 1)
	checkExec(t, results, "B", 1)
}

func checkReachable(t *testing.T, results []solver.ExecResult, name string, expected bool) {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			if r.Reachable != expected {
				t.Errorf("Node %s: expected reachable=%v, got reachable=%v", name, expected, r.Reachable)
			}
			return
		}
	}
	t.Errorf("Node %s not found in results", name)
}

func getExec(results []solver.ExecResult, name string) int64 {
	for _, r := range results {
		if r.Name == name {
			return r.Exec
		}
	}
	return -1
}

// Ensure test-only helpers exist (add to pipeline package)
func init() {
	// Create temp dir for test
	os.MkdirAll(filepath.Join(os.TempDir(), "pipeline-maxhit-test"), 0755)
}
