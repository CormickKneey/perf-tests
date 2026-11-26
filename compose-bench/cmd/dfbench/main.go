// Command dfbench is the compose-bench CLI.
//
// Host side:      dfbench compose {run,smoke,preflight,render,clean,index}
// In containers:  dfbench fixtures | driver {serve,probe} | runner | compare
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/bench"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: dfbench <command> [flags]

commands:
  compose run|smoke|preflight|clean|index   host-side orchestration (docker compose)
  compare                                    offline Mann-Whitney comparison of a run
  fixtures                                   generate reproducible fixture files (container)
  driver serve|probe                         dfget sidecar HTTP API (container)
  runner                                     run the measurements of one block (container)
  validate [--schemas DIR] <results dir>     check results JSON against schemas/
  version
`)
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "fixtures":
		return cmdFixtures(args[1:])
	case "driver":
		return cmdDriver(ctx, args[1:])
	case "runner":
		return cmdRunner(ctx, args[1:])
	case "compare":
		return cmdCompare(args[1:])
	case "compose":
		return cmdCompose(ctx, args[1:])
	case "validate":
		return cmdValidate(args[1:])
	case "version":
		fmt.Println("dfbench", version)
		return 0
	case "-h", "--help", "help":
		usage()
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
	usage()
	return 2
}

var version = "dev"

func cmdFixtures(args []string) int {
	fs := flag.NewFlagSet("fixtures", flag.ExitOnError)
	dir := fs.String("dir", "/fixtures", "output directory (fixtures volume)")
	sizes := fs.String("sizes", "8MiB", "comma separated sizes")
	seed := fs.String("seed", "dfperf", "deterministic seed")
	fs.Parse(args)
	list, err := bench.ParseSizes(*sizes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err := bench.GenerateFixtures(*dir, *seed, list, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "fixtures:", err)
		return 1
	}
	return 0
}

func cmdDriver(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "driver serve|probe")
		return 2
	}
	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("driver serve", flag.ExitOnError)
		listen := fs.String("listen", ":7070", "listen address")
		endpoint := fs.String("endpoint", "/var/run/dragonfly/dfdaemon.sock", "dfdaemon unix socket")
		workdir := fs.String("workdir", "/var/lib/dragonfly/dfperf-out", "output dir on the daemon's storage volume")
		dfget := fs.String("dfget", "/usr/local/bin/dfget", "dfget binary")
		fs.Parse(args[1:])
		d := &bench.Driver{DfgetPath: *dfget, Endpoint: *endpoint, Workdir: *workdir}
		srv := &http.Server{Addr: *listen, Handler: d.Handler(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			srv.Shutdown(sctx)
		}()
		fmt.Printf("driver: listening on %s, endpoint %s\n", *listen, *endpoint)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "probe":
		fs := flag.NewFlagSet("driver probe", flag.ExitOnError)
		addr := fs.String("addr", "127.0.0.1:7070", "driver address")
		fs.Parse(args[1:])
		c := &bench.DriverClient{Base: "http://" + *addr, HTTP: &http.Client{Timeout: 2 * time.Second}}
		if _, err := c.Get(ctx, "/v1/healthz"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "driver serve|probe")
	return 2
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	schemas := fs.String("schemas", "schemas", "schema directory")
	fs.Parse(args)
	root := "results"
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	bad, n, err := results.ValidateResults(*schemas, root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for f, errs := range bad {
		for _, e := range errs {
			fmt.Printf("%s: %s\n", f, e)
		}
	}
	fmt.Printf("validated %d documents, %d with violations\n", n, len(bad))
	if len(bad) > 0 {
		return 1
	}
	return 0
}
