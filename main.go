package main

import (
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/ocsin1/pipeline-maxhit/internal/graph"
	"github.com/ocsin1/pipeline-maxhit/internal/pipeline"
	"github.com/ocsin1/pipeline-maxhit/internal/report"
	"github.com/ocsin1/pipeline-maxhit/internal/solver"
)

func main() {
	cfg := parseFlags()
	if cfg == nil {
		return
	}

	base := parseBasePipeline(cfg)
	plan := analyzeOptions(base, cfg)

	allResults := solveAllCombos(base, cfg.entry, plan)

	// Merge: take per-node max exec across all combos.
	final := mergeResults(allResults)
	sccWarnings := solver.FindSCCs(graph.Build(base, cfg.entry)).Warnings
	report.Print(os.Stdout, final, sccWarnings)
}

// --- config ---

type config struct {
	pipelineDir string
	defaults    string
	entry       string
	taskFile    string
	taskName    string
}

func parseFlags() *config {
	pipelineDir := flag.String("pipeline", "", "Path to pipeline JSON directory")
	defaults := flag.String("defaults", "", "Path to default_pipeline.json")
	entry := flag.String("entry", "", "Entry node name")
	task := flag.String("task", "", "Path to task interface JSON")
	taskName := flag.String("task-name", "", "Task name (default: first in file)")
	listTasks := flag.Bool("list-tasks", false, "List tasks in task file and exit")
	flag.Parse()

	if *task != "" && *listTasks {
		listTaskEntries(*task)
		return nil
	}

	cfg := &config{
		pipelineDir: *pipelineDir,
		defaults:    *defaults,
		entry:       *entry,
		taskFile:    *task,
		taskName:    *taskName,
	}

	if cfg.taskFile != "" {
		tf := mustParseTask(cfg.taskFile)
		td := resolveTask(tf, cfg.taskName)
		if cfg.entry == "" {
			cfg.entry = td.Entry
		}
		if cfg.taskName == "" {
			cfg.taskName = td.Name
		}
		fmt.Fprintf(os.Stderr, "Task: %s, Entry: %s\n", td.Name, cfg.entry)
	}

	if cfg.pipelineDir == "" || cfg.entry == "" {
		fmt.Fprintf(os.Stderr, "Usage: pipeline-maxhit -pipeline <dir> (-entry <node> | -task <file>)\n")
		os.Exit(1)
	}
	return cfg
}

func mustParseTask(path string) *pipeline.TaskFile {
	tf, err := pipeline.ParseTaskFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return tf
}

func resolveTask(tf *pipeline.TaskFile, name string) *pipeline.TaskDef {
	if name == "" && len(tf.Tasks) > 0 {
		return &tf.Tasks[0]
	}
	td := tf.FindTask(name)
	if td == nil {
		fmt.Fprintf(os.Stderr, "Task %q not found\n", name)
		os.Exit(1)
	}
	return td
}

func listTaskEntries(path string) {
	tf := mustParseTask(path)
	for _, t := range tf.Tasks {
		fmt.Printf("  %s (entry: %s)\n", t.Name, t.Entry)
	}
}

// --- pipeline + override ---

func parseBasePipeline(cfg *config) *pipeline.Pipeline {
	p, err := pipeline.ParsePipeline(cfg.pipelineDir, cfg.defaults)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Parsed %d nodes\n", len(p.Nodes))
	return p
}

func analyzeOptions(base *pipeline.Pipeline, cfg *config) *pipeline.OverridePlan {
	if cfg.taskFile == "" {
		return nil
	}
	tf := mustParseTask(cfg.taskFile)
	plan := pipeline.AnalyzeOptions(tf, cfg.taskName)
	if plan == nil {
		return nil
	}
	// Apply non-struct (union) overrides to all nodes now.
	// Struct-modifying overrides will be applied per-combo.
	base.ApplyOverridesUnion(tf, cfg.taskName)
	fmt.Fprintf(os.Stderr, "Override combos: %d\n", len(plan.EnumerateCombos()))
	return plan
}

// --- solve across combos ---

func solveAllCombos(base *pipeline.Pipeline, entry string, plan *pipeline.OverridePlan) [][]solver.ExecResult {
	combos := []pipeline.OverrideCombo{{}} // default: no struct overrides
	if plan != nil {
		combos = plan.EnumerateCombos()
	}

	var allResults [][]solver.ExecResult
	for i, combo := range combos {
		p := clonePipeline(base)
		p.ApplyCombo(combo)

		g := graph.Build(p, entry)
		graph.ResolveAnchors(g)
		g.ReapplyBlockerPreprocessing()
		g.RecomputeReachability()

		fn := solver.BuildFlowNetwork(g)
		fn.MaxFlow()
		results := solver.ExtractResults(fn, g)

		if len(combos) > 1 {
			reachable := countReachable(g)
			fmt.Fprintf(os.Stderr, "Combo %d/%d: %d reachable          \r", i+1, len(combos), reachable)
		}
		allResults = append(allResults, results)
	}
	if len(combos) > 1 {
		fmt.Fprintln(os.Stderr)
	}
	return allResults
}

func mergeResults(all [][]solver.ExecResult) []solver.ExecResult {
	if len(all) == 0 {
		return nil
	}
	merged := slices.Clone(all[0])
	for _, batch := range all[1:] {
		for i := range merged {
			if batch[i].Exec > merged[i].Exec {
				merged[i] = batch[i]
			}
		}
	}
	return merged
}

// --- helpers ---

func clonePipeline(p *pipeline.Pipeline) *pipeline.Pipeline {
	clone := &pipeline.Pipeline{
		Nodes:    make(map[string]*pipeline.NodeData, len(p.Nodes)),
		Defaults: p.Defaults,
	}
	for name, nd := range p.Nodes {
		copy := *nd
		copy.Next = slices.Clone(nd.Next)
		copy.OnError = slices.Clone(nd.OnError)
		if nd.Anchors != nil {
			copy.Anchors = make(map[string]string, len(nd.Anchors))
			for k, v := range nd.Anchors {
				copy.Anchors[k] = v
			}
		}
		clone.Nodes[name] = &copy
	}
	return clone
}

func countReachable(g *graph.Graph) int {
	count := 0
	for _, info := range g.Nodes {
		if info.Reachable {
			count++
		}
	}
	return count
}
