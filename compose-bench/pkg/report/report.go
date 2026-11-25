// Package report turns the raw block results of a run into report.json and
// report.md and decides the verdict / exit code.
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/bench"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/stats"
)

// Exit codes (SPEC): 0 pass, 1 regression, 2 benchmark failed, 3 infra/invalid,
// 4 inconclusive. With several outcomes the priority is 3 > 2 > 1 > 4 > 0.
const (
	ExitPass         = 0
	ExitRegression   = 1
	ExitFailed       = 2
	ExitInvalid      = 3
	ExitInconclusive = 4
)

// Verdict names per exit code.
var verdictNames = map[int]string{
	ExitPass: "PASS", ExitRegression: "REGRESSION", ExitFailed: "FAILED",
	ExitInvalid: "INVALID", ExitInconclusive: "INCONCLUSIVE",
}

// VerdictName returns the verdict string of an exit code.
func VerdictName(code int) string { return verdictNames[code] }

var priority = []int{ExitInvalid, ExitFailed, ExitRegression, ExitInconclusive, ExitPass}

// Combine returns the highest priority exit code among codes (0 if none).
func Combine(codes ...int) int {
	for _, p := range priority {
		for _, c := range codes {
			if c == p {
				return p
			}
		}
	}
	return ExitPass
}

// Policy is compose/compare.yaml.
type Policy struct {
	Alpha               float64 `yaml:"alpha" json:"alpha"`
	Metric              string  `yaml:"metric" json:"metric"`
	RegressionThreshold float64 `yaml:"regression_threshold" json:"regression_threshold"`
	MinAbsDeltaMs       float64 `yaml:"min_abs_delta_ms" json:"min_abs_delta_ms"`
	MinSamples          int     `yaml:"min_samples" json:"min_samples"`
	MinBlocks           int     `yaml:"min_blocks" json:"min_blocks"`
	MaxFailureRate      float64 `yaml:"max_failure_rate" json:"max_failure_rate"`
	Bootstrap           struct {
		Level      float64 `yaml:"level" json:"level"`
		Iterations int     `yaml:"iterations" json:"iterations"`
		Seed       uint64  `yaml:"seed" json:"seed"`
	} `yaml:"bootstrap" json:"bootstrap"`
}

// DefaultPolicy mirrors compose/compare.yaml.
func DefaultPolicy() Policy {
	p := Policy{Alpha: 0.05, Metric: "duration_ms", RegressionThreshold: 0.10, MinAbsDeltaMs: 5,
		MinSamples: 8, MinBlocks: 4, MaxFailureRate: 0.01}
	p.Bootstrap.Level, p.Bootstrap.Iterations, p.Bootstrap.Seed = 0.95, 2000, 20261121
	return p
}

// LoadPolicy reads a policy file; missing keys keep defaults.
func LoadPolicy(path string) (Policy, error) {
	p := DefaultPolicy()
	data, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.Metric != "duration_ms" {
		return p, fmt.Errorf("unsupported metric %q", p.Metric)
	}
	return p, nil
}

// VariantStats aggregates one variant.
type VariantStats struct {
	Blocks      int     `json:"blocks"`
	ValidBlocks int     `json:"valid_blocks"`
	Measured    int     `json:"measured"`
	Failures    int     `json:"failures"`
	FailureRate float64 `json:"failure_rate"`
}

// Group is one (state, file) comparison.
type Group struct {
	State              string         `json:"state"`
	File               string         `json:"file"`
	SizeBytes          int64          `json:"size_bytes"`
	Baseline           stats.Summary  `json:"baseline"`
	Candidate          *stats.Summary `json:"candidate,omitempty"`
	BaselineMiBps      stats.Summary  `json:"baseline_mib_per_s"`
	CandidateMiBps     *stats.Summary `json:"candidate_mib_per_s,omitempty"`
	DeltaPct           float64        `json:"delta_pct"`
	DeltaMs            float64        `json:"delta_ms"`
	P                  float64        `json:"p"`
	PAdj               float64        `json:"p_adj"`
	Method             string         `json:"method,omitempty"`
	RatioCILo          float64        `json:"ratio_ci_lo"`
	RatioCIHi          float64        `json:"ratio_ci_hi"`
	Verdict            string         `json:"verdict"`
	Reason             string         `json:"reason"`
	BaselineDurations  []float64      `json:"baseline_durations_ms"`
	CandidateDurations []float64      `json:"candidate_durations_ms,omitempty"`
}

// BlockSummary is a compact view of a block for the report.
type BlockSummary struct {
	ID       string          `json:"id"`
	Variant  string          `json:"variant"`
	Index    int             `json:"index"`
	Order    int             `json:"order"`
	Status   string          `json:"status"`
	Reasons  []string        `json:"reasons,omitempty"`
	Samples  int             `json:"samples"`
	Failures int             `json:"failures"`
	Checks   []results.Check `json:"checks"`
}

// Report is results/<run>/report.json.
type Report struct {
	Schema      string                  `json:"schema"`
	Run         string                  `json:"run"`
	Mode        string                  `json:"mode"`
	Profile     string                  `json:"profile"`
	GeneratedAt time.Time               `json:"generated_at"`
	Verdict     string                  `json:"verdict"`
	ExitCode    int                     `json:"exit_code"`
	Reasons     []string                `json:"reasons"`
	NonBaseline bool                    `json:"non_baseline"`
	Policy      Policy                  `json:"policy"`
	Variants    map[string]VariantStats `json:"variants"`
	Groups      []Group                 `json:"groups"`
	Blocks      []BlockSummary          `json:"blocks"`
}

type blockData struct {
	block   results.Block
	samples []results.Sample
}

func loadBlocks(runDir string) ([]blockData, error) {
	dirs, err := filepath.Glob(filepath.Join(runDir, "blocks", "*", "block.json"))
	if err != nil {
		return nil, err
	}
	var out []blockData
	for _, p := range dirs {
		var b results.Block
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		samples, err := bench.ReadSamples(filepath.Join(filepath.Dir(p), "samples.jsonl"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		out = append(out, blockData{block: b, samples: samples})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].block.Order < out[j].block.Order })
	return out, nil
}

// LoadRun reads results/<run>/run.json.
func LoadRun(runDir string) (*results.Run, error) {
	data, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		return nil, err
	}
	var r results.Run
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

type groupKey struct{ state, file string }

// Build computes the report of a run directory.
func Build(runDir string, policy Policy, run *results.Run) (*Report, error) {
	blocks, err := loadBlocks(runDir)
	if err != nil {
		return nil, err
	}
	rep := &Report{
		Schema: results.SchemaVersion, Run: run.ID, Mode: run.Mode, Profile: run.Profile,
		GeneratedAt: time.Now().UTC(), NonBaseline: run.NonBaseline, Policy: policy,
		Variants: map[string]VariantStats{}, Reasons: []string{},
	}
	var codes []int
	if len(blocks) == 0 {
		rep.Reasons = append(rep.Reasons, "no blocks were executed")
		codes = append(codes, ExitInvalid)
	}

	durations := map[string]map[groupKey][]float64{results.VariantBase: {}, results.VariantCand: {}}
	mibps := map[string]map[groupKey][]float64{results.VariantBase: {}, results.VariantCand: {}}
	sizes := map[groupKey]int64{}
	for _, bd := range blocks {
		b := bd.block
		vs := rep.Variants[b.Variant]
		vs.Blocks++
		if b.Status == results.BlockOK {
			vs.ValidBlocks++
		} else {
			codes = append(codes, ExitInvalid)
			rep.Reasons = append(rep.Reasons, fmt.Sprintf("block %s is %s: %s", b.ID, b.Status, strings.Join(b.Reasons, "; ")))
		}
		rep.Blocks = append(rep.Blocks, BlockSummary{ID: b.ID, Variant: b.Variant, Index: b.Index, Order: b.Order,
			Status: b.Status, Reasons: b.Reasons, Samples: b.Samples, Failures: b.Failures, Checks: b.Checks})
		for _, s := range bd.samples {
			if s.Role != "measure" {
				if !s.OK && s.Role == "prep" {
					vs.Failures++
					vs.Measured++
				}
				continue
			}
			vs.Measured++
			if !s.OK {
				vs.Failures++
				continue
			}
			if b.Status != results.BlockOK {
				continue // never compare samples from invalid blocks
			}
			k := groupKey{s.State, s.File}
			sizes[k] = s.SizeBytes
			durations[s.Variant][k] = append(durations[s.Variant][k], s.DurationMs)
			mibps[s.Variant][k] = append(mibps[s.Variant][k], s.MiBps)
		}
		rep.Variants[b.Variant] = vs
	}
	for name, vs := range rep.Variants {
		if vs.Measured > 0 {
			vs.FailureRate = float64(vs.Failures) / float64(vs.Measured)
		}
		rep.Variants[name] = vs
		if vs.Measured > 0 && vs.FailureRate >= policy.MaxFailureRate {
			codes = append(codes, ExitFailed)
			rep.Reasons = append(rep.Reasons, fmt.Sprintf("%s download failure rate %.4f >= %.2f (%d/%d)", name, vs.FailureRate, policy.MaxFailureRate, vs.Failures, vs.Measured))
		}
	}

	keys := make([]groupKey, 0, len(sizes))
	for k := range sizes {
		keys = append(keys, k)
	}
	stateOrder := map[string]int{}
	for i, s := range results.AllStates {
		stateOrder[s] = i
	}
	sort.Slice(keys, func(i, j int) bool {
		if sizes[keys[i]] != sizes[keys[j]] {
			return sizes[keys[i]] < sizes[keys[j]]
		}
		return stateOrder[keys[i].state] < stateOrder[keys[j].state]
	})

	compare := run.Mode == results.ModeCompare
	if compare {
		for _, v := range []string{results.VariantBase, results.VariantCand} {
			if rep.Variants[v].ValidBlocks < policy.MinBlocks {
				codes = append(codes, ExitInvalid)
				rep.Reasons = append(rep.Reasons, fmt.Sprintf("%s has %d valid blocks, policy requires %d", v, rep.Variants[v].ValidBlocks, policy.MinBlocks))
			}
		}
	}
	var ps []float64
	for _, k := range keys {
		b, c := durations[results.VariantBase][k], durations[results.VariantCand][k]
		g := Group{State: k.state, File: k.file, SizeBytes: sizes[k],
			Baseline: stats.Describe(b), BaselineMiBps: stats.Describe(mibps[results.VariantBase][k]),
			BaselineDurations: round3(b), P: math.NaN(), PAdj: math.NaN(), RatioCILo: math.NaN(), RatioCIHi: math.NaN()}
		if compare {
			cs, cm := stats.Describe(c), stats.Describe(mibps[results.VariantCand][k])
			g.Candidate, g.CandidateMiBps = &cs, &cm
			g.CandidateDurations = round3(c)
			mw := stats.MannWhitney(b, c)
			g.P, g.Method = mw.P, mw.Method
			if len(b) > 0 && len(c) > 0 && g.Baseline.Median > 0 {
				g.DeltaMs = cs.Median - g.Baseline.Median
				g.DeltaPct = 100 * g.DeltaMs / g.Baseline.Median
				g.RatioCILo, g.RatioCIHi = stats.RatioCI(b, c, policy.Bootstrap.Level, policy.Bootstrap.Iterations, policy.Bootstrap.Seed)
			}
		}
		ps = append(ps, g.P)
		rep.Groups = append(rep.Groups, g)
	}
	if compare && len(ps) > 0 {
		clean := make([]float64, len(ps))
		for i, p := range ps {
			clean[i] = p
			if math.IsNaN(p) {
				clean[i] = 1
			}
		}
		adj := stats.Holm(clean)
		for i := range rep.Groups {
			g := &rep.Groups[i]
			g.PAdj = adj[i]
			g.Verdict, g.Reason = decide(*g, policy)
			switch g.Verdict {
			case "regression":
				codes = append(codes, ExitRegression)
			case "inconclusive":
				codes = append(codes, ExitInconclusive)
			}
		}
	} else {
		for i := range rep.Groups {
			rep.Groups[i].Verdict, rep.Groups[i].Reason = "measured", "single-version smoke run"
			if rep.Groups[i].Baseline.N == 0 {
				rep.Groups[i].Verdict = "inconclusive"
			}
		}
		if len(rep.Groups) == 0 && len(blocks) > 0 {
			codes = append(codes, ExitInvalid)
			rep.Reasons = append(rep.Reasons, "no successful measured samples")
		}
	}
	rep.ExitCode = Combine(codes...)
	rep.Verdict = VerdictName(rep.ExitCode)
	if run.Mode == results.ModeSmoke && rep.ExitCode == ExitPass {
		rep.Verdict = "PASS"
	}
	return rep, nil
}

// decide classifies one group.
func decide(g Group, p Policy) (string, string) {
	if g.Candidate == nil {
		return "inconclusive", "no candidate samples"
	}
	if g.Baseline.N < p.MinSamples || g.Candidate.N < p.MinSamples {
		return "inconclusive", fmt.Sprintf("insufficient samples (%d vs %d, need %d)", g.Baseline.N, g.Candidate.N, p.MinSamples)
	}
	slower := g.DeltaPct/100 > p.RegressionThreshold && g.DeltaMs > p.MinAbsDeltaMs
	faster := g.DeltaPct/100 < -p.RegressionThreshold && -g.DeltaMs > p.MinAbsDeltaMs
	sig := g.PAdj < p.Alpha
	switch {
	case sig && slower:
		return "regression", fmt.Sprintf("candidate median %.1f%% slower (p_adj=%.3g < %.2g)", g.DeltaPct, g.PAdj, p.Alpha)
	case slower:
		return "inconclusive", fmt.Sprintf("candidate median %.1f%% slower but not significant (p_adj=%.3g)", g.DeltaPct, g.PAdj)
	case sig && faster:
		return "improvement", fmt.Sprintf("candidate median %.1f%% faster (p_adj=%.3g)", -g.DeltaPct, g.PAdj)
	case sig:
		return "pass", fmt.Sprintf("significant but below practical threshold (%.1f%%, %.2fms)", g.DeltaPct, g.DeltaMs)
	}
	return "pass", fmt.Sprintf("no significant difference (Δ %.1f%%, p_adj=%.3g)", g.DeltaPct, g.PAdj)
}

func round3(xs []float64) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = math.Round(x*1000) / 1000
	}
	return out
}

// Write stores report.json and report.md. NaN is not valid JSON, so NaN and
// Inf values are written as null.
func Write(runDir string, rep *Report, run *results.Run) error {
	data, err := json.MarshalIndent(nanSafe(rep), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "report.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, "report.md"), []byte(Markdown(rep, run)), 0o644)
}

// nanSafe converts the report into a generic value with NaN/Inf -> nil.
func nanSafe(rep *Report) any {
	type alias Report
	groups := make([]map[string]any, 0, len(rep.Groups))
	for _, g := range rep.Groups {
		m := map[string]any{}
		m["state"], m["file"], m["size_bytes"] = g.State, g.File, g.SizeBytes
		m["baseline"], m["baseline_mib_per_s"] = g.Baseline, g.BaselineMiBps
		if g.Candidate != nil {
			m["candidate"], m["candidate_mib_per_s"] = g.Candidate, g.CandidateMiBps
			m["candidate_durations_ms"] = g.CandidateDurations
		}
		m["delta_pct"], m["delta_ms"] = num(g.DeltaPct), num(g.DeltaMs)
		m["p"], m["p_adj"], m["method"] = num(g.P), num(g.PAdj), g.Method
		m["ratio_ci_lo"], m["ratio_ci_hi"] = num(g.RatioCILo), num(g.RatioCIHi)
		m["verdict"], m["reason"] = g.Verdict, g.Reason
		m["baseline_durations_ms"] = g.BaselineDurations
		groups = append(groups, m)
	}
	return struct {
		*alias
		Groups []map[string]any `json:"groups"`
	}{(*alias)(rep), groups}
}

func num(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return math.Round(f*1e6) / 1e6
}
