package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

// IndexEntry summarizes one run for the dashboard (results/index.json).
type IndexEntry struct {
	ID             string    `json:"id"`
	Mode           string    `json:"mode"`
	Profile        string    `json:"profile"`
	Verdict        string    `json:"verdict"`
	ExitCode       int       `json:"exit_code"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	NonBaseline    bool      `json:"non_baseline"`
	Classification string    `json:"classification"`
	Baseline       string    `json:"baseline"`
	Candidate      string    `json:"candidate,omitempty"`
	Overlays       []string  `json:"overlays,omitempty"`
	Blocks         int       `json:"blocks"`
	Reasons        []string  `json:"reasons,omitempty"`
	HasReport      bool      `json:"has_report"`
}

// Index is results/index.json.
type Index struct {
	Schema      string       `json:"schema"`
	GeneratedAt time.Time    `json:"generated_at"`
	Runs        []IndexEntry `json:"runs"`
}

func variantLabel(v results.Variant) string {
	return v.Scheduler.Ref + " [" + v.Scheduler.Version + "] + " + v.Client.Ref + " [" + v.Client.Version + "]"
}

// WriteIndex rebuilds results/index.json from every results/*/run.json.
func WriteIndex(root string) error {
	paths, err := filepath.Glob(filepath.Join(root, "*", "run.json"))
	if err != nil {
		return err
	}
	idx := Index{Schema: results.SchemaVersion, GeneratedAt: time.Now().UTC(), Runs: []IndexEntry{}}
	for _, p := range paths {
		run, err := LoadRun(filepath.Dir(p))
		if err != nil {
			continue
		}
		e := IndexEntry{ID: run.ID, Mode: run.Mode, Profile: run.Profile, Verdict: run.Verdict, ExitCode: run.ExitCode,
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, NonBaseline: run.NonBaseline,
			Classification: run.Host.Classification, Baseline: variantLabel(run.Baseline),
			Overlays: run.Baseline.Overlays, Blocks: len(run.BlockOrder), Reasons: run.Notes}
		if run.Candidate != nil {
			e.Candidate = variantLabel(*run.Candidate)
			if len(run.Candidate.Overlays) > len(e.Overlays) {
				e.Overlays = run.Candidate.Overlays
			}
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(p), "report.json")); err == nil {
			e.HasReport = true
		}
		idx.Runs = append(idx.Runs, e)
	}
	sort.Slice(idx.Runs, func(i, j int) bool { return idx.Runs[i].ID > idx.Runs[j].ID })
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "index.json"), append(data, '\n'), 0o644)
}
