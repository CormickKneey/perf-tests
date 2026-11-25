package report

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/bench"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

func TestCombinePriority(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{nil, ExitPass},
		{[]int{ExitPass}, ExitPass},
		{[]int{ExitInconclusive, ExitPass}, ExitInconclusive},
		{[]int{ExitInconclusive, ExitRegression}, ExitRegression},
		{[]int{ExitRegression, ExitFailed}, ExitFailed},
		{[]int{ExitFailed, ExitInvalid, ExitRegression, ExitInconclusive}, ExitInvalid},
	}
	for _, c := range cases {
		if got := Combine(c.in...); got != c.want {
			t.Errorf("Combine(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// fakeRun writes a synthetic run directory. slow multiplies candidate
// durations; failEvery marks every n-th candidate sample as failed.
func fakeRun(t *testing.T, mode string, blocks int, slow float64, failEvery int, invalid bool) (string, *results.Run) {
	t.Helper()
	dir := t.TempDir()
	run := &results.Run{ID: "testrun01", Mode: mode, Profile: "file-3peers"}
	rng := rand.New(rand.NewPCG(1, 2))
	variants := []string{results.VariantBase}
	if mode == results.ModeCompare {
		variants = append(variants, results.VariantCand)
	}
	order := 0
	count := 0
	for bi := 1; bi <= blocks; bi++ {
		for _, v := range variants {
			order++
			id := fmt.Sprintf("%02d-%s-b%d", order, v, bi)
			bdir := filepath.Join(dir, "blocks", id)
			if err := os.MkdirAll(bdir, 0o755); err != nil {
				t.Fatal(err)
			}
			b := results.Block{ID: id, Variant: v, Index: bi, Order: order, Status: results.BlockOK}
			if invalid && order == 1 {
				b.Status, b.Reasons = results.BlockInvalid, []string{"seed-announced: client went back-to-source"}
			}
			f, _ := os.Create(filepath.Join(bdir, "samples.jsonl"))
			for _, state := range results.AllStates {
				for i := 0; i < 3; i++ {
					d := 100 + rng.Float64()*10
					if v == results.VariantCand {
						d *= slow
					}
					count++
					s := results.Sample{Variant: v, Block: bi, Role: "measure", State: state, File: "f-4mib.bin",
						SizeBytes: 4 << 20, OK: true, SHA256OK: true, DurationMs: d, MiBps: 4 / (d / 1000)}
					if v == results.VariantCand && failEvery > 0 && count%failEvery == 0 {
						s.OK = false
					}
					line, _ := json.Marshal(s)
					f.Write(append(line, '\n'))
				}
			}
			f.Close()
			if err := bench.WriteJSON(filepath.Join(bdir, "block.json"), b); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir, run
}

func build(t *testing.T, dir string, run *results.Run) *Report {
	t.Helper()
	rep, err := Build(dir, DefaultPolicy(), run)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, rep, run); err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestAAIsPass(t *testing.T) {
	dir, run := fakeRun(t, results.ModeCompare, 4, 1.0, 0, false)
	rep := build(t, dir, run)
	if rep.ExitCode != ExitPass && rep.ExitCode != ExitInconclusive {
		t.Fatalf("A/A verdict %s (%v)", rep.Verdict, rep.Reasons)
	}
	for _, g := range rep.Groups {
		if g.Verdict == "regression" {
			t.Fatalf("A/A regression in %+v", g)
		}
	}
	// report.json must be valid JSON even with NaN-free/NaN fields.
	data, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("report.json: %v", err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "report.md"))
	if !strings.Contains(string(md), "Verdict:") {
		t.Fatal("markdown missing verdict")
	}
}

func TestRegressionDetected(t *testing.T) {
	dir, run := fakeRun(t, results.ModeCompare, 4, 1.5, 0, false)
	rep := build(t, dir, run)
	if rep.ExitCode != ExitRegression {
		t.Fatalf("want regression, got %s (%v)", rep.Verdict, rep.Reasons)
	}
}

func TestImprovementIsPass(t *testing.T) {
	dir, run := fakeRun(t, results.ModeCompare, 4, 0.6, 0, false)
	rep := build(t, dir, run)
	if rep.ExitCode != ExitPass {
		t.Fatalf("want pass, got %s", rep.Verdict)
	}
	if rep.Groups[0].Verdict != "improvement" {
		t.Fatalf("group verdict %s", rep.Groups[0].Verdict)
	}
}

func TestFailureRateBeatsRegression(t *testing.T) {
	dir, run := fakeRun(t, results.ModeCompare, 4, 1.5, 10, false)
	rep := build(t, dir, run)
	if rep.ExitCode != ExitFailed {
		t.Fatalf("want failed (2), got %d %s", rep.ExitCode, rep.Verdict)
	}
}

func TestInvalidBlockWins(t *testing.T) {
	dir, run := fakeRun(t, results.ModeCompare, 4, 1.5, 10, true)
	rep := build(t, dir, run)
	if rep.ExitCode != ExitInvalid {
		t.Fatalf("want invalid (3), got %d", rep.ExitCode)
	}
}

func TestTooFewBlocksInvalid(t *testing.T) {
	dir, run := fakeRun(t, results.ModeCompare, 2, 1.0, 0, false)
	rep := build(t, dir, run)
	if rep.ExitCode != ExitInvalid {
		t.Fatalf("want invalid with 2 blocks per variant, got %d", rep.ExitCode)
	}
}

func TestInsufficientSamplesInconclusive(t *testing.T) {
	dir, run := fakeRun(t, results.ModeCompare, 4, 1.0, 0, false)
	p := DefaultPolicy()
	p.MinSamples = 100
	rep, err := Build(dir, p, run)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode != ExitInconclusive {
		t.Fatalf("want inconclusive, got %d", rep.ExitCode)
	}
}

func TestSmokePass(t *testing.T) {
	dir, run := fakeRun(t, results.ModeSmoke, 1, 1.0, 0, false)
	rep := build(t, dir, run)
	if rep.ExitCode != ExitPass || rep.Verdict != "PASS" {
		t.Fatalf("smoke %d %s", rep.ExitCode, rep.Verdict)
	}
	if err := WriteIndex(filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPolicy(t *testing.T) {
	p, err := LoadPolicy("../../compose/compare.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if p.Alpha != 0.05 || p.MaxFailureRate != 0.01 || p.MinBlocks != 4 || p.Bootstrap.Iterations != 2000 {
		t.Fatalf("policy %+v", p)
	}
}
