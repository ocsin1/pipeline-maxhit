package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

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
	allFinal := runAllTasks(base, cfg)

	sccWarnings := solver.FindSCCs(graph.Build(base, allFinal[0].Entry)).Warnings
	report.Print(os.Stdout, allFinal, sccWarnings)
}

// --- task plan ---

type taskPlan struct {
	Name    string
	Entry   string
	Plan    *pipeline.OverridePlan
	TaskDef *pipeline.TaskDef
	TF      *pipeline.TaskFile
}

// --- config ---

type config struct {
	pipelineDir string
	defaults    string
	entry       string
	taskFile    string
	taskNames   []string
	allTasks    bool
}

func parseFlags() *config {
	pipelineDir := flag.String("pipeline", "", "Pipeline JSON 目录路径")
	defaults := flag.String("defaults", "", "default_pipeline.json 路径")
	entry := flag.String("entry", "", "入口节点名")
	task := flag.String("task", "", "任务接口 JSON 文件路径")
	taskName := flag.String("task-name", "", "任务名，逗号分隔多个，\"all\" 运行全部")
	listTasks := flag.Bool("list-tasks", false, "列出任务后退出")
	flag.Parse()

	if *task != "" && *listTasks {
		tf := mustParseTask(*task)
		for _, t := range tf.Tasks {
			fmt.Printf("  %s (entry: %s)\n", t.Name, t.Entry)
		}
		return nil
	}

	cfg := &config{
		pipelineDir: *pipelineDir,
		defaults:    *defaults,
		entry:       *entry,
		taskFile:    *task,
	}

	if *taskName == "all" {
		cfg.allTasks = true
	} else if *taskName != "" {
		for _, n := range strings.Split(*taskName, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				cfg.taskNames = append(cfg.taskNames, n)
			}
		}
	}

	if cfg.pipelineDir == "" || (cfg.entry == "" && cfg.taskFile == "") {
		fmt.Fprintf(os.Stderr, "用法: pipeline-maxhit -pipeline <dir> (-entry <node> | -task <file>)\n")
		os.Exit(1)
	}
	return cfg
}

// --- pipeline ---

func parseBasePipeline(cfg *config) *pipeline.Pipeline {
	p, err := pipeline.ParsePipeline(cfg.pipelineDir, cfg.defaults)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "解析 %d 个节点\n", len(p.Nodes))
	return p
}

func mustParseTask(path string) *pipeline.TaskFile {
	tf, err := pipeline.ParseTaskFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
	return tf
}

// --- run all tasks ---

func runAllTasks(base *pipeline.Pipeline, cfg *config) []report.TaskResult {
	if cfg.taskFile == "" {
		// Single entry, no task file.
		return []report.TaskResult{runOne(base, cfg.entry, cfg.entry, nil, nil)}
	}

	tf := mustParseTask(cfg.taskFile)
	plans := resolvePlans(tf, cfg)

	var results []report.TaskResult
	for i, tp := range plans {
		fmt.Fprintf(os.Stderr, "\n任务 %d/%d: %s\n", i+1, len(plans), tp.Name)
		tr := runOne(base, tp.Name, tp.Entry, tp.Plan, tp.TF)
		results = append(results, tr)
	}
	return results
}

func resolvePlans(tf *pipeline.TaskFile, cfg *config) []taskPlan {
	if cfg.allTasks {
		// All tasks in file.
		var plans []taskPlan
		for i := range tf.Tasks {
			plans = append(plans, taskPlan{
				Name:    tf.Tasks[i].Name,
				Entry:   tf.Tasks[i].Entry,
				Plan:    pipeline.AnalyzeOptions(tf, tf.Tasks[i].Name),
				TaskDef: &tf.Tasks[i],
				TF:      tf,
			})
		}
		return plans
	}

	if len(cfg.taskNames) > 0 {
		var plans []taskPlan
		for _, name := range cfg.taskNames {
			td := tf.FindTask(name)
			if td == nil {
				fmt.Fprintf(os.Stderr, "任务 %q 未找到\n", name)
				continue
			}
			plans = append(plans, taskPlan{
				Name:    td.Name,
				Entry:   td.Entry,
				Plan:    pipeline.AnalyzeOptions(tf, td.Name),
				TaskDef: td,
				TF:      tf,
			})
		}
		return plans
	}

	// Default: first task.
	td := &tf.Tasks[0]
	return []taskPlan{{
		Name:    td.Name,
		Entry:   td.Entry,
		Plan:    pipeline.AnalyzeOptions(tf, td.Name),
		TaskDef: td,
		TF:      tf,
	}}
}

func runOne(base *pipeline.Pipeline, taskName, entry string, plan *pipeline.OverridePlan, tf *pipeline.TaskFile) report.TaskResult {
	p := clonePipeline(base)
	if plan != nil {
		fmt.Fprintf(os.Stderr, "  覆盖组合: %d\n", len(plan.EnumerateCombos()))
		p.ApplyOverridesUnion(tf, taskName)
	}

	allResults := solveAllCombos(p, entry, plan)
	merged := mergeResults(allResults)
	return report.TaskResult{Name: taskName, Entry: entry, Results: merged}
}

// --- solve ---

func solveAllCombos(base *pipeline.Pipeline, entry string, plan *pipeline.OverridePlan) [][]solver.ExecResult {
	combos := []pipeline.OverrideCombo{{}}
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
			fmt.Fprintf(os.Stderr, "  组合 %d/%d: %d 可达          \r", i+1, len(combos), countReachable(g))
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
	// Build name-indexed map from the first batch.
	byName := make(map[string]*solver.ExecResult, len(all[0]))
	for i := range all[0] {
		byName[all[0][i].Name] = &all[0][i]
	}
	// Merge subsequent batches by name.
	for _, batch := range all[1:] {
		for i := range batch {
			if cur, ok := byName[batch[i].Name]; ok {
				if batch[i].Exec > cur.Exec {
					*cur = batch[i]
				}
			}
		}
	}
	// Return in stable order (sorted by name).
	result := make([]solver.ExecResult, 0, len(byName))
	for _, r := range byName {
		result = append(result, *r)
	}
	return result
}

// --- helpers ---

func clonePipeline(p *pipeline.Pipeline) *pipeline.Pipeline {
	clone := &pipeline.Pipeline{
		Nodes:    make(map[string]*pipeline.NodeData, len(p.Nodes)),
		Defaults: p.Defaults,
	}
	for name, nd := range p.Nodes {
		cp := *nd
		cp.Next = slices.Clone(nd.Next)
		cp.OnError = slices.Clone(nd.OnError)
		if nd.Anchors != nil {
			cp.Anchors = make(map[string]string, len(nd.Anchors))
			for k, v := range nd.Anchors {
				cp.Anchors[k] = v
			}
		}
		clone.Nodes[name] = &cp
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
