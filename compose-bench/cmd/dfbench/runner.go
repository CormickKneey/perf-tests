package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/bench"
)

func cmdRunner(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("runner", flag.ExitOnError)
	planPath := fs.String("plan", "/out/plan.json", "plan written by the orchestrator")
	out := fs.String("out", "/out", "block output directory")
	fs.Parse(args)
	data, err := os.ReadFile(*planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		return bench.RunnerError
	}
	var plan bench.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		fmt.Fprintln(os.Stderr, "runner: plan:", err)
		return bench.RunnerError
	}
	logf, err := os.Create(filepath.Join(*out, "runner.log"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		return bench.RunnerError
	}
	defer logf.Close()
	r := &bench.Runner{Plan: plan, Out: *out, Log: io.MultiWriter(os.Stdout, logf)}
	return r.Run(ctx)
}
