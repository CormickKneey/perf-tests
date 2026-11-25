package compose

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/bench"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/report"
	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

// Options configures a benchmark run.
type Options struct {
	Root        string // repository root
	ResultsRoot string
	Mode        string // smoke | compare
	Profile     string
	Baseline    string
	Candidate   string
	Overlays    []string // applied to every variant
	// CandidateOverlays are applied to the candidate only.
	CandidateOverlays []string
	Set               map[string]string // profile overrides (DFPERF_* keys)
	RunID             string
	CranePath         string
	LinuxBinary       string // dfbench built for linux/<engine arch>
	Log               io.Writer
}

// Profile is a parsed compose/profiles/<name>.env.
type Profile map[string]string

// LoadProfile reads KEY=VALUE lines.
func LoadProfile(path string) (Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p := Profile{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s: bad line %q", path, line)
		}
		p[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return p, sc.Err()
}

func (p Profile) Int(k string, def int) int {
	if v, err := strconv.Atoi(p[k]); err == nil {
		return v
	}
	return def
}

func (p Profile) Str(k, def string) string {
	if v := p[k]; v != "" {
		return v
	}
	return def
}

// ParseRef turns a variant ref into scheduler and client image refs:
//
//	scheduler=<image>,client=<image>   explicit images (tags or digests)
//	<sched-tag>+<client-tag>           dragonflyoss/{scheduler,client} tags
//	<tag>                              same tag for both (e.g. latest)
func ParseRef(ref string) (scheduler, client string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", errors.New("empty ref")
	}
	if strings.Contains(ref, "=") {
		for _, part := range strings.Split(ref, ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok || v == "" {
				return "", "", fmt.Errorf("bad ref part %q", part)
			}
			switch k {
			case "scheduler":
				scheduler = v
			case "client":
				client = v
			default:
				return "", "", fmt.Errorf("unknown component %q in ref", k)
			}
		}
		if scheduler == "" || client == "" {
			return "", "", fmt.Errorf("ref %q must set scheduler= and client=", ref)
		}
		return scheduler, client, nil
	}
	expand := func(repo, t string) string {
		if strings.ContainsAny(t, "/@") {
			return t
		}
		return "dragonflyoss/" + repo + ":" + t
	}
	if s, c, ok := strings.Cut(ref, "+"); ok {
		return expand("scheduler", s), expand("client", c), nil
	}
	return expand("scheduler", ref), expand("client", ref), nil
}

type variantSetup struct {
	meta      results.Variant
	configDir string
}

// Orchestrator runs one benchmark run.
type Orchestrator struct {
	o        Options
	x        *Exec
	im       *Images
	prof     Profile
	runDir   string
	run      results.Run
	topo     Topology
	variants map[string]*variantSetup
	tools    results.Image
	origin   results.Image
	log      io.Writer
}

func (r *Orchestrator) logf(format string, a ...any) {
	fmt.Fprintf(r.log, time.Now().Format("15:04:05")+" "+format+"\n", a...)
}

// Run executes the whole pipeline and returns the process exit code.
func Run(ctx context.Context, o Options) int {
	if o.RunID == "" {
		o.RunID = NewRunID(time.Now())
	}
	if err := ValidateRunID(o.RunID); err != nil {
		fmt.Fprintln(o.Log, err)
		return report.ExitInvalid
	}
	runDir := filepath.Join(o.ResultsRoot, o.RunID)
	if _, err := os.Stat(runDir); err == nil {
		fmt.Fprintf(o.Log, "run dir %s already exists\n", runDir)
		return report.ExitInvalid
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		fmt.Fprintln(o.Log, err)
		return report.ExitInvalid
	}
	console, err := os.Create(filepath.Join(runDir, "console.log"))
	if err != nil {
		fmt.Fprintln(o.Log, err)
		return report.ExitInvalid
	}
	defer console.Close()
	log := io.MultiWriter(o.Log, console)
	r := &Orchestrator{o: o, runDir: runDir, log: log, x: &Exec{Log: log}, variants: map[string]*variantSetup{}}
	code := r.execute(ctx)
	r.logf("run %s finished: verdict %s, exit code %d", o.RunID, r.run.Verdict, code)
	r.logf("results: %s", runDir)
	return code
}

func (r *Orchestrator) execute(ctx context.Context) int {
	o := r.o
	r.run = results.Run{
		Schema: results.SchemaVersion, ID: o.RunID, Mode: o.Mode, Profile: o.Profile,
		StartedAt: time.Now().UTC(), Status: "running", Params: map[string]string{},
	}
	r.run.GitCommit, r.run.GitDirty = gitInfo(ctx, r.x, o.Root)
	var pre []string
	fail := func(stage string, err error) int {
		r.logf("%s failed: %v", stage, err)
		pre = append(pre, fmt.Sprintf("%s: %v", stage, err))
		return r.finish(ctx, pre)
	}

	prof, err := LoadProfile(filepath.Join(o.Root, "compose", "profiles", o.Profile+".env"))
	if err != nil {
		return fail("profile", err)
	}
	for k, v := range o.Set {
		prof[k] = v
	}
	r.prof = prof
	for k, v := range prof {
		r.run.Params[k] = v
	}
	r.topo = Topology{NetPrefix: prof.Str("DFPERF_NET_PREFIX", "10.213.0")}

	di, err := DockerInfo(ctx, r.x)
	if err != nil {
		return fail("docker", err)
	}
	arch := EngineArch(di.Architecture)
	r.im = &Images{X: r.x, Arch: arch, CranePath: o.CranePath, WorkDir: filepath.Join(o.Root, ".build", "images")}
	_ = os.MkdirAll(r.im.WorkDir, 0o755)

	// Support images.
	toolsBase, err := r.im.Resolve(ctx, "tools-base", prof.Str("DFPERF_TOOLS_BASE_IMAGE", "alpine:3.20"))
	if err != nil {
		return fail("resolve tools base image", err)
	}
	r.origin, err = r.im.Resolve(ctx, "origin", prof.Str("DFPERF_ORIGIN_IMAGE", "nginx:stable-alpine"))
	if err != nil {
		return fail("resolve origin image", err)
	}
	r.tools, err = r.im.BuildDerived(ctx, "tools", toolsBase.Pinned, "dfperf-tools", o.LinuxBinary)
	if err != nil {
		return fail("build tools image", err)
	}
	r.run.Support = []results.Image{toolsBase, r.tools, r.origin}

	// Preflight.
	pf := RunPreflight(ctx, r.x, di, PreflightOptions{
		ResultsDir: r.runDir, NetPrefix: r.topo.NetPrefix,
		MinFreeGiB: float64(prof.Int("DFPERF_MIN_FREE_GIB", 5)), DiskProbeImg: toolsBase.Pinned,
	})
	_ = bench.WriteJSON(filepath.Join(r.runDir, "preflight.json"), pf)
	r.run.Host = pf.Host
	r.run.NonBaseline = !pf.Host.BaselineEligible
	for _, c := range pf.Checks {
		r.logf("preflight %-26s ok=%-5v %s", c.Name, c.OK, c.Detail)
		if c.Fatal && !c.OK {
			pre = append(pre, "preflight "+c.Name+": "+c.Detail)
		}
	}
	if !pf.OK() {
		return r.finish(ctx, pre)
	}

	// Variants.
	names := []string{results.VariantBase}
	refs := map[string]string{results.VariantBase: o.Baseline}
	if o.Mode == results.ModeCompare {
		names = append(names, results.VariantCand)
		refs[results.VariantCand] = o.Candidate
	}
	for _, name := range names {
		vs, err := r.setupVariant(ctx, name, refs[name])
		if err != nil {
			return fail("variant "+name, err)
		}
		r.variants[name] = vs
	}
	r.run.Baseline = r.variants[results.VariantBase].meta
	if c, ok := r.variants[results.VariantCand]; ok {
		m := c.meta
		r.run.Candidate = &m
	}

	// Fixtures volume (external, survives `down -v`).
	sizes, err := bench.ParseSizes(prof.Str("DFPERF_FILE_SIZES", "4MiB"))
	if err != nil {
		return fail("fixtures", err)
	}
	seed := prof.Str("DFPERF_FIXTURE_SEED", "dfperf")
	vol := bench.FixtureVolumeName(seed, sizes)
	if err := ValidateFixturesVolume(vol); err != nil {
		return fail("fixtures", err)
	}
	if _, err := r.x.Output(ctx, "docker", "volume", "inspect", vol); err != nil {
		if _, err := r.x.Output(ctx, "docker", "volume", "create", "--label", "dfperf.fixtures=1",
			"--label", "dfperf.fixtures.spec="+bench.FixtureSpecHash(seed, sizes), vol); err != nil {
			return fail("fixtures volume", err)
		}
		r.logf("created fixtures volume %s", vol)
	} else {
		r.logf("reusing fixtures volume %s", vol)
	}
	r.run.Fixtures = results.Fixtures{Schema: results.SchemaVersion, Volume: vol, SpecHash: bench.FixtureSpecHash(seed, sizes), Seed: seed, Generator: bench.FixtureGenerator}
	for _, s := range sizes {
		r.run.Fixtures.Files = append(r.run.Fixtures.Files, results.FixtureFile{Name: bench.FixtureName(s), Path: "/files/" + bench.FixtureName(s), Size: s, SHA256: bench.ExpectedSHA256(seed, s)})
	}

	// Archive inputs.
	for _, f := range []string{"compose.yaml", "compare.yaml"} {
		_ = copyFile(filepath.Join(o.Root, "compose", f), filepath.Join(r.runDir, f), 0o644)
	}
	_ = copyFile(filepath.Join(o.Root, "compose", "profiles", o.Profile+".env"), filepath.Join(r.runDir, "profile.env"), 0o644)

	// Blocks.
	blocks := prof.Int("DFPERF_BLOCKS", 4)
	if o.Mode == results.ModeSmoke {
		blocks = prof.Int("DFPERF_SMOKE_BLOCKS", 1)
	}
	order := ABBAOrder(blocks, o.Mode == results.ModeCompare)
	for _, s := range order {
		r.run.BlockOrder = append(r.run.BlockOrder, s.ID())
	}
	r.logf("block order (%s): %s", OrderString(order), strings.Join(r.run.BlockOrder, " "))
	r.writeRun()

	for _, slot := range order {
		if ctx.Err() != nil {
			pre = append(pre, "interrupted")
			break
		}
		b := r.runBlock(ctx, slot)
		if b.Status != results.BlockOK {
			r.logf("block %s is %s (%s); aborting remaining blocks: the run is invalid", b.ID, b.Status, strings.Join(b.Reasons, "; "))
			pre = append(pre, fmt.Sprintf("aborted after block %s (%s)", b.ID, b.Status))
			break
		}
	}
	return r.finish(ctx, pre)
}

func (r *Orchestrator) setupVariant(ctx context.Context, name, ref string) (*variantSetup, error) {
	schedRef, clientRef, err := ParseRef(ref)
	if err != nil {
		return nil, err
	}
	vs := &variantSetup{meta: results.Variant{Name: name}}
	if vs.meta.Scheduler, err = r.im.Resolve(ctx, "scheduler", schedRef); err != nil {
		return nil, err
	}
	if vs.meta.Client, err = r.im.Resolve(ctx, "client", clientRef); err != nil {
		return nil, err
	}
	vs.meta.Scheduler.Version = r.im.Version(ctx, "scheduler", vs.meta.Scheduler.Pinned)
	vs.meta.Client.Version = r.im.Version(ctx, "client", vs.meta.Client.Pinned)
	if vs.meta.Driver, err = r.im.BuildDerived(ctx, "driver", vs.meta.Client.Pinned, "dfperf-driver", r.o.LinuxBinary); err != nil {
		return nil, err
	}
	vs.meta.Driver.Version = r.im.Version(ctx, "driver", vs.meta.Driver.Pinned)
	if vs.meta.Driver.Version != vs.meta.Client.Version {
		return nil, fmt.Errorf("driver dfget %q != client %q", vs.meta.Driver.Version, vs.meta.Client.Version)
	}
	overlays := append([]string{}, r.o.Overlays...)
	if name == results.VariantCand {
		overlays = append(overlays, r.o.CandidateOverlays...)
	}
	vs.meta.Overlays = overlays
	vs.configDir = filepath.Join(r.runDir, "config", name)
	applied, err := RenderConfigs(filepath.Join(r.o.Root, "compose", "config"), filepath.Join(r.o.Root, "compose", "config", "overlays"), vs.configDir, r.topo, overlays)
	if err != nil {
		return nil, err
	}
	r.logf("variant %s: scheduler %s [%s], client %s [%s], driver %s, overlays %v",
		name, vs.meta.Scheduler.Pinned, vs.meta.Scheduler.Version, vs.meta.Client.Pinned, vs.meta.Client.Version, vs.meta.Driver.Pinned, applied)
	return vs, nil
}

func (r *Orchestrator) composeArgs(project string, extra ...string) []string {
	args := []string{"compose", "-p", project, "-f", filepath.Join(r.o.Root, "compose", "compose.yaml"),
		"--project-directory", filepath.Join(r.o.Root, "compose")}
	return append(args, extra...)
}

func (r *Orchestrator) blockEnv(slot BlockSlot, blockDir string) []string {
	vs := r.variants[slot.Variant]
	env := map[string]string{
		"DFPERF_RUN_ID":          r.o.RunID,
		"DFPERF_VARIANT":         slot.Variant,
		"DFPERF_BLOCK":           strconv.Itoa(slot.Index),
		"DFPERF_SCHEDULER_IMAGE": vs.meta.Scheduler.Pinned,
		"DFPERF_CLIENT_IMAGE":    vs.meta.Client.Pinned,
		"DFPERF_DRIVER_IMAGE":    vs.meta.Driver.Pinned,
		"DFPERF_TOOLS_IMAGE":     r.tools.Pinned,
		"DFPERF_ORIGIN_IMAGE":    r.origin.Pinned,
		"DFPERF_FILE_SIZES":      r.prof.Str("DFPERF_FILE_SIZES", "4MiB"),
		"DFPERF_FIXTURE_SEED":    r.run.Fixtures.Seed,
		"DFPERF_CONFIG_DIR":      vs.configDir,
		"DFPERF_BLOCK_DIR":       blockDir,
		"DFPERF_RUN_DIR":         r.runDir,
		"DFPERF_NET_PREFIX":      r.topo.NetPrefix,
		"DFPERF_FIXTURES_VOLUME": r.run.Fixtures.Volume,
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func (r *Orchestrator) plan(slot BlockSlot) bench.Plan {
	t := r.topo
	states := strings.Split(r.prof.Str("DFPERF_STATES", strings.Join(results.AllStates, ",")), ",")
	p := bench.Plan{
		Run: r.o.RunID, Variant: slot.Variant, Block: slot.Index, BlockID: fmt.Sprintf("%s-b%d", slot.Variant[:4], slot.Index),
		OriginURL: "http://" + t.IP("origin"), Seed: "seed-client", Clients: t.Clients(),
		Drivers: map[string]string{"seed-client": "http://" + t.IP("driver-seed") + ":7070"},
		Metrics: map[string]string{"scheduler": "http://" + t.IP("scheduler") + ":8000/metrics"},
		PeerIPs: map[string]string{}, States: states, Samples: r.prof.Int("DFPERF_SAMPLES", 3),
		SchedulingPolicy:       r.prof.Str("DFPERF_SCHEDULING_POLICY", "always"),
		DownloadTimeoutSeconds: r.prof.Int("DFPERF_DOWNLOAD_TIMEOUT", 600),
		WarmupTimeoutSeconds:   r.prof.Int("DFPERF_WARMUP_TIMEOUT", 45),
		ReadyTimeoutSeconds:    60,
		OriginLog:              "/var/log/dfperf-origin/access.jsonl",
		ManifestPath:           "/fixtures/manifest.json",
	}
	for peer := range t.Peers() {
		p.Metrics[peer] = "http://" + t.IP(peer) + ":4002/metrics"
		p.PeerIPs[peer] = t.IP(peer)
	}
	for i, c := range t.Clients() {
		p.Drivers[c] = fmt.Sprintf("http://%s:7070", t.IP(fmt.Sprintf("driver-%d", i+1)))
	}
	return p
}

func (r *Orchestrator) runBlock(ctx context.Context, slot BlockSlot) results.Block {
	b := results.Block{Schema: results.SchemaVersion, Run: r.o.RunID, ID: slot.ID(), Variant: slot.Variant,
		Index: slot.Index, Order: slot.Order, StartedAt: time.Now().UTC(), Status: results.BlockInfraFail, RunnerExitCode: -1}
	blockDir := filepath.Join(r.runDir, "blocks", slot.ID())
	logDir := filepath.Join(blockDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	defer func() {
		b.FinishedAt = time.Now().UTC()
		_ = bench.WriteJSON(filepath.Join(blockDir, "block.json"), b)
	}()
	project, err := ProjectName(r.o.RunID, slot.Variant, slot.Index)
	if err != nil {
		b.Reasons = append(b.Reasons, err.Error())
		return b
	}
	b.Project = project
	r.logf("=== block %d/%d %s (project %s)", slot.Order, len(r.run.BlockOrder), slot.ID(), project)
	env := r.blockEnv(slot, blockDir)
	_ = os.WriteFile(filepath.Join(blockDir, "compose.env"), []byte(strings.Join(env, "\n")+"\n"), 0o644)
	if err := bench.WriteJSON(filepath.Join(blockDir, "plan.json"), r.plan(slot)); err != nil {
		b.Reasons = append(b.Reasons, err.Error())
		return b
	}
	rendered, err := r.x.OutputEnv(ctx, env, "docker", r.composeArgs(project, "--profile", "tools", "config")...)
	if err != nil {
		b.Reasons = append(b.Reasons, "compose config: "+err.Error())
		return b
	}
	_ = os.WriteFile(filepath.Join(blockDir, "compose.rendered.yaml"), []byte(rendered+"\n"), 0o644)

	// Always clean up, even on cancellation.
	defer func() {
		start := time.Now()
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer cancel()
		r.collect(cctx, project, env, blockDir)
		if err := r.down(cctx, project, env, logDir); err != nil {
			b.Reasons = append(b.Reasons, "cleanup: "+err.Error())
			b.Status = results.BlockInfraFail
		}
		b.CleanupSeconds = time.Since(start).Seconds()
	}()

	start := time.Now()
	upLog, _ := os.Create(filepath.Join(logDir, "up.log"))
	code, err := r.x.Stream(ctx, upLog, env, "docker", r.composeArgs(project, "up", "-d", "--wait",
		"--wait-timeout", strconv.Itoa(r.prof.Int("DFPERF_UP_TIMEOUT", 180)))...)
	upLog.Close()
	b.UpSeconds = time.Since(start).Seconds()
	if err != nil || code != 0 {
		detail := fmt.Sprintf("exit %d", code)
		if err != nil {
			detail = err.Error()
		}
		b.Reasons = append(b.Reasons, fmt.Sprintf("compose up --wait failed (%s), see logs/up.log and logs/compose.log", detail))
		r.logf("compose up failed for %s", project)
		return b
	}
	r.logf("environment healthy in %.1fs", b.UpSeconds)

	start = time.Now()
	runLog, _ := os.Create(filepath.Join(logDir, "runner.stdout.log"))
	code, err = r.x.Stream(ctx, io.MultiWriter(runLog, prefixWriter(r.log, "    ")), env, "docker",
		r.composeArgs(project, "--profile", "tools", "run", "--rm", "--no-deps", "-T", "runner")...)
	runLog.Close()
	b.RunnerSeconds = time.Since(start).Seconds()
	b.RunnerExitCode = code
	var rr bench.RunnerReport
	data, rerr := os.ReadFile(filepath.Join(blockDir, "runner.json"))
	if rerr == nil {
		rerr = json.Unmarshal(data, &rr)
	}
	if rerr != nil {
		b.Reasons = append(b.Reasons, fmt.Sprintf("runner exit %d without runner.json: %v", code, rerr))
		return b
	}
	b.Checks, b.Samples, b.Failures = rr.Checks, rr.Samples, rr.Failures
	b.Reasons = append(b.Reasons, rr.Reasons...)
	switch {
	case code == bench.RunnerOK && rr.Status == results.BlockOK:
		b.Status = results.BlockOK
	case code == bench.RunnerInvalid:
		b.Status = results.BlockInvalid
	default:
		b.Status = results.BlockInfraFail
		b.Reasons = append(b.Reasons, fmt.Sprintf("runner exit code %d", code))
	}
	return b
}

func (r *Orchestrator) collect(ctx context.Context, project string, env []string, blockDir string) {
	if out, err := r.x.OutputEnv(ctx, env, "docker", r.composeArgs(project, "--profile", "*", "ps", "-a", "--format", "json")...); err == nil {
		_ = os.WriteFile(filepath.Join(blockDir, "ps.jsonl"), []byte(out+"\n"), 0o644)
	}
	if out, err := r.x.OutputEnv(ctx, env, "docker", r.composeArgs(project, "images", "--format", "json")...); err == nil {
		_ = os.WriteFile(filepath.Join(blockDir, "images.json"), []byte(out+"\n"), 0o644)
	}
	f, err := os.Create(filepath.Join(blockDir, "logs", "compose.log"))
	if err == nil {
		r.x.Stream(ctx, f, env, "docker", r.composeArgs(project, "--profile", "*", "logs", "--no-color", "--timestamps")...)
		f.Close()
	}
}

// down removes the project including its named volumes. It refuses names
// that do not match the dfperf project pattern and verifies nothing remains.
func (r *Orchestrator) down(ctx context.Context, project string, env []string, logDir string) error {
	if err := ValidateProject(project); err != nil {
		return err
	}
	f, _ := os.Create(filepath.Join(logDir, "down.log"))
	defer f.Close()
	code, err := r.x.Stream(ctx, f, env, "docker", r.composeArgs(project, "--profile", "*", "down", "-v", "--remove-orphans", "-t", "5")...)
	if err != nil || code != 0 {
		return fmt.Errorf("compose down exit %d: %v", code, err)
	}
	return VerifyGone(ctx, r.x, project)
}

// VerifyGone checks that no container, volume or network of project remains.
func VerifyGone(ctx context.Context, x *Exec, project string) error {
	filter := "label=com.docker.compose.project=" + project
	var left []string
	for _, kind := range [][]string{{"ps", "-aq"}, {"volume", "ls", "-q"}, {"network", "ls", "-q"}} {
		args := append(append([]string{}, kind...), "--filter", filter)
		out, err := x.Output(ctx, "docker", args...)
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) != "" {
			left = append(left, kind[0]+":"+strings.Join(strings.Fields(out), ","))
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("leftover resources of %s: %s", project, strings.Join(left, " "))
	}
	return nil
}

func (r *Orchestrator) writeRun() {
	_ = bench.WriteJSON(filepath.Join(r.runDir, "run.json"), r.run)
}

func (r *Orchestrator) finish(ctx context.Context, pre []string) int {
	policyPath := filepath.Join(r.o.Root, "compose", "compare.yaml")
	policy, err := report.LoadPolicy(policyPath)
	if err != nil {
		pre = append(pre, "compare policy: "+err.Error())
	}
	if minBlocks := r.prof.Int("DFPERF_MIN_BLOCKS", 0); minBlocks > 0 {
		policy.MinBlocks = minBlocks
	}
	r.run.FinishedAt = time.Now().UTC()
	r.writeRun()
	rep, err := report.Build(r.runDir, policy, &r.run)
	if err != nil {
		rep = &report.Report{Schema: results.SchemaVersion, Run: r.run.ID, Mode: r.run.Mode, Policy: policy}
		pre = append(pre, "report: "+err.Error())
	}
	if len(pre) > 0 {
		rep.Reasons = append(pre, rep.Reasons...)
		rep.ExitCode = report.Combine(rep.ExitCode, report.ExitInvalid)
		rep.Verdict = report.VerdictName(rep.ExitCode)
	}
	r.run.ExitCode, r.run.Verdict = rep.ExitCode, rep.Verdict
	r.run.Status = "finished"
	r.run.Notes = rep.Reasons
	r.writeRun()
	if err := report.Write(r.runDir, rep, &r.run); err != nil {
		r.logf("write report: %v", err)
	}
	_ = os.WriteFile(filepath.Join(r.runDir, "exit_code"), []byte(strconv.Itoa(rep.ExitCode)+"\n"), 0o644)
	if err := report.WriteIndex(r.o.ResultsRoot); err != nil {
		r.logf("index: %v", err)
	}
	for _, reason := range rep.Reasons {
		r.logf("reason: %s", reason)
	}
	return rep.ExitCode
}

func gitInfo(ctx context.Context, x *Exec, root string) (string, bool) {
	commit, err := x.Output(ctx, "git", "-C", root, "rev-parse", "--short=12", "HEAD")
	if err != nil {
		return "unknown", false
	}
	st, _ := x.Output(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=no")
	return commit, strings.TrimSpace(st) != ""
}

type prefixW struct {
	w      io.Writer
	prefix string
	bol    bool
}

func prefixWriter(w io.Writer, prefix string) io.Writer {
	return &prefixW{w: w, prefix: prefix, bol: true}
}

func (p *prefixW) Write(b []byte) (int, error) {
	var out []byte
	for _, c := range b {
		if p.bol {
			out = append(out, p.prefix...)
			p.bol = false
		}
		out = append(out, c)
		if c == '\n' {
			p.bol = true
		}
	}
	if _, err := p.w.Write(out); err != nil {
		return 0, err
	}
	return len(b), nil
}
