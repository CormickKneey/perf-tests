package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/compose"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/report"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func cmdCompose(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "dfbench compose run|smoke|clean|index|preflight")
		return 2
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("compose "+sub, flag.ExitOnError)
	root := fs.String("root", ".", "repository root")
	resultsDir := fs.String("results", "", "results root (default <root>/results)")
	profile := fs.String("profile", "file-3peers", "compose/profiles/<name>.env")
	baseline := fs.String("baseline", "", "baseline ref: <sched-tag>+<client-tag> | scheduler=<img>,client=<img> | <tag>")
	candidate := fs.String("candidate", "", "candidate ref (run only)")
	ref := fs.String("ref", "", "version ref for smoke (alias of --baseline)")
	runID := fs.String("run-id", "", "explicit run id ([0-9a-z]{8,})")
	linuxBin := fs.String("linux-binary", "", "dfbench built for linux/<engine arch>")
	crane := fs.String("crane", "", "crane binary for the host-side pull fallback")
	fixtures := fs.Bool("fixtures", false, "clean: also remove dfperf-fixtures-* volumes")
	dryRun := fs.Bool("dry-run", false, "clean: only print what would be removed")
	var overlays, candOverlays, sets multiFlag
	fs.Var(&overlays, "overlay", "config overlay applied to all variants (repeatable)")
	fs.Var(&candOverlays, "candidate-overlay", "config overlay applied to the candidate only (repeatable)")
	fs.Var(&sets, "set", "override a profile value KEY=VALUE (repeatable)")
	fs.Parse(args)

	absRoot, _ := filepath.Abs(*root)
	if *resultsDir == "" {
		*resultsDir = filepath.Join(absRoot, "results")
	}
	absResults, _ := filepath.Abs(*resultsDir)
	x := &compose.Exec{Log: os.Stdout}

	switch sub {
	case "run", "smoke":
		o := compose.Options{Root: absRoot, ResultsRoot: absResults, Profile: *profile, RunID: *runID,
			CranePath: *crane, LinuxBinary: *linuxBin, Log: os.Stdout, Overlays: overlays,
			CandidateOverlays: candOverlays, Set: map[string]string{}}
		for _, s := range sets {
			k, v, ok := strings.Cut(s, "=")
			if !ok {
				fmt.Fprintf(os.Stderr, "--set %q: want KEY=VALUE\n", s)
				return report.ExitInvalid
			}
			o.Set[k] = v
		}
		if sub == "smoke" {
			o.Mode = results.ModeSmoke
			o.Baseline = *ref
			if o.Baseline == "" {
				o.Baseline = *baseline
			}
			if o.Baseline == "" {
				fmt.Fprintln(os.Stderr, "smoke needs --ref")
				return report.ExitInvalid
			}
		} else {
			o.Mode = results.ModeCompare
			o.Baseline, o.Candidate = *baseline, *candidate
			if o.Baseline == "" || o.Candidate == "" {
				fmt.Fprintln(os.Stderr, "run needs --baseline and --candidate")
				return report.ExitInvalid
			}
		}
		if o.LinuxBinary == "" {
			fmt.Fprintln(os.Stderr, "--linux-binary is required (use scripts/compose-bench.sh)")
			return report.ExitInvalid
		}
		return compose.Run(ctx, o)
	case "clean":
		if err := compose.Clean(ctx, x, *runID, *fixtures, *dryRun); err != nil {
			fmt.Fprintln(os.Stderr, "clean:", err)
			return 1
		}
		return 0
	case "index":
		if err := report.WriteIndex(absResults); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(filepath.Join(absResults, "index.json"))
		return 0
	case "preflight":
		prof, err := compose.LoadProfile(filepath.Join(absRoot, "compose", "profiles", *profile+".env"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return report.ExitInvalid
		}
		di, err := compose.DockerInfo(ctx, x)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return report.ExitInvalid
		}
		pf := compose.RunPreflight(ctx, x, di, compose.PreflightOptions{ResultsDir: absResults,
			NetPrefix: prof.Str("DFPERF_NET_PREFIX", "10.213.0"), MinFreeGiB: float64(prof.Int("DFPERF_MIN_FREE_GIB", 5))})
		for _, c := range pf.Checks {
			fmt.Printf("%-26s ok=%-5v fatal=%-5v %s\n", c.Name, c.OK, c.Fatal, c.Detail)
		}
		if !pf.OK() {
			return report.ExitInvalid
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown compose subcommand %q\n", sub)
	return 2
}

func cmdCompare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	runDir := fs.String("run-dir", "", "results/<run> directory")
	cfg := fs.String("config", "", "comparison policy (default <run-dir>/compare.yaml)")
	fs.Parse(args)
	if *runDir == "" {
		fmt.Fprintln(os.Stderr, "--run-dir is required")
		return report.ExitInvalid
	}
	if *cfg == "" {
		*cfg = filepath.Join(*runDir, "compare.yaml")
	}
	policy, err := report.LoadPolicy(*cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "policy:", err)
		return report.ExitInvalid
	}
	run, err := report.LoadRun(*runDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run:", err)
		return report.ExitInvalid
	}
	if v, ok := run.Params["DFPERF_MIN_BLOCKS"]; ok && v != "" {
		fmt.Sscan(v, &policy.MinBlocks)
	}
	rep, err := report.Build(*runDir, policy, run)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		return report.ExitInvalid
	}
	// Reasons recorded by the orchestrator (preflight, aborted blocks) stay.
	for _, n := range run.Notes {
		if !contains(rep.Reasons, n) {
			rep.Reasons = append(rep.Reasons, n)
		}
	}
	if run.ExitCode == report.ExitInvalid {
		rep.ExitCode = report.Combine(rep.ExitCode, report.ExitInvalid)
		rep.Verdict = report.VerdictName(rep.ExitCode)
	}
	if err := report.Write(*runDir, rep, run); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return report.ExitInvalid
	}
	fmt.Printf("%s: %s (exit %d)\n", run.ID, rep.Verdict, rep.ExitCode)
	return rep.ExitCode
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
