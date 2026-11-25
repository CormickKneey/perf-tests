package report

import (
	"fmt"
	"math"
	"strings"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

func f1(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "–"
	}
	return fmt.Sprintf("%.1f", v)
}

func pfmt(v float64) string {
	if math.IsNaN(v) {
		return "–"
	}
	if v < 0.001 {
		return fmt.Sprintf("%.1e", v)
	}
	return fmt.Sprintf("%.3f", v)
}

func short(d string) string {
	if i := strings.Index(d, "sha256:"); i >= 0 && len(d) >= i+7+12 {
		return d[:i] + "sha256:" + d[i+7:i+7+12]
	}
	return d
}

// Markdown renders report.md.
func Markdown(rep *Report, run *results.Run) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("# compose-bench %s report — %s\n\n", rep.Mode, rep.Run)
	w("**Verdict: %s** (exit code %d)\n\n", rep.Verdict, rep.ExitCode)
	if rep.NonBaseline {
		w("> **non-baseline**: %s. Results are a functional smoke signal only; formal baselines require a fixed Linux host.\n\n", run.Host.Note)
	}
	if len(rep.Reasons) > 0 {
		w("Reasons:\n\n")
		for _, r := range rep.Reasons {
			w("- %s\n", r)
		}
		w("\n")
	}
	w("## Versions\n\n| variant | component | ref | pinned | version |\n|---|---|---|---|---|\n")
	vars := []*results.Variant{&run.Baseline}
	if run.Candidate != nil {
		vars = append(vars, run.Candidate)
	}
	for _, v := range vars {
		for _, im := range []results.Image{v.Scheduler, v.Client, v.Driver} {
			w("| %s | %s | `%s` | `%s` | %s |\n", v.Name, im.Component, im.Ref, short(im.Pinned), im.Version)
		}
		if len(v.Overlays) > 0 {
			w("| %s | overlays | %s | | |\n", v.Name, strings.Join(v.Overlays, ", "))
		}
	}
	w("\n## Environment\n\n")
	h := run.Host
	w("- host: %s (%s, %s, %d CPUs)\n- docker: %s on %s (kernel %s), compose %s\n- classification: **%s**, baseline eligible: %v\n- profile: `%s`, fixtures volume `%s`\n- git: %s%s\n\n",
		h.Hostname, h.OS, h.Arch, h.CPUs, h.DockerVersion, h.DockerOS, h.DockerKernel, h.ComposeVersion,
		h.Classification, h.BaselineEligible, run.Profile, run.Fixtures.Volume, run.GitCommit, map[bool]string{true: " (dirty)", false: ""}[run.GitDirty])
	w("## Blocks (execution order)\n\n| # | block | status | measured | failures | notes |\n|---|---|---|---|---|---|\n")
	for _, bl := range rep.Blocks {
		w("| %d | %s | %s | %d | %d | %s |\n", bl.Order, bl.ID, bl.Status, bl.Samples, bl.Failures, strings.Join(bl.Reasons, "; "))
	}
	w("\n## Failure rates\n\n| variant | valid blocks | measured | failures | rate |\n|---|---|---|---|---|\n")
	for _, v := range []string{results.VariantBase, results.VariantCand} {
		if vs, ok := rep.Variants[v]; ok {
			w("| %s | %d/%d | %d | %d | %.4f |\n", v, vs.ValidBlocks, vs.Blocks, vs.Measured, vs.Failures, vs.FailureRate)
		}
	}
	if rep.Mode == results.ModeCompare {
		w("\n## Comparison (duration ms, lower is better)\n\nMann–Whitney U two-sided, Holm-corrected; regression needs p_adj < %.2g and Δ > %.0f%% and > %.0f ms.\n\n",
			rep.Policy.Alpha, rep.Policy.RegressionThreshold*100, rep.Policy.MinAbsDeltaMs)
		w("| state | file | n (b/c) | median b | median c | Δ | ratio 95%% CI | p | p_adj | verdict |\n|---|---|---|---|---|---|---|---|---|---|\n")
		for _, g := range rep.Groups {
			cn, cm := 0, math.NaN()
			if g.Candidate != nil {
				cn, cm = g.Candidate.N, g.Candidate.Median
			}
			w("| %s | %s | %d/%d | %s | %s | %s%% | %s–%s | %s | %s | **%s** |\n", g.State, g.File, g.Baseline.N, cn,
				f1(g.Baseline.Median), f1(cm), f1(g.DeltaPct), fmt.Sprintf("%.2f", g.RatioCILo), fmt.Sprintf("%.2f", g.RatioCIHi),
				pfmt(g.P), pfmt(g.PAdj), g.Verdict)
		}
	} else {
		w("\n## Measurements\n\n| state | file | n | p10 ms | median ms | p90 ms | median MiB/s |\n|---|---|---|---|---|---|---|\n")
		for _, g := range rep.Groups {
			w("| %s | %s | %d | %s | %s | %s | %s |\n", g.State, g.File, g.Baseline.N, f1(g.Baseline.P10), f1(g.Baseline.Median), f1(g.Baseline.P90), f1(g.BaselineMiBps.Median))
		}
	}
	w("\n## Validity checks\n\n")
	for _, bl := range rep.Blocks {
		for _, c := range bl.Checks {
			mark := "ok"
			if !c.OK {
				mark = "FAIL"
			}
			req := ""
			if !c.Required {
				req = " (informational)"
			}
			w("- %s `%s` %s%s: %s\n", bl.ID, c.Name, mark, req, c.Detail)
		}
	}
	w("\nBlock order: %s\n", strings.Join(run.BlockOrder, " → "))
	return b.String()
}
