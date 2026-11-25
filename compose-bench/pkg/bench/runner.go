package bench

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
	"strings"
	"time"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

// Plan is written by the host orchestrator to <block>/plan.json and read by
// the runner container. Everything the runner needs is in here; it has no
// docker.sock and only talks to peers over the dfperf network.
type Plan struct {
	Run                    string            `json:"run"`
	Variant                string            `json:"variant"`
	Block                  int               `json:"block"`
	BlockID                string            `json:"block_id"`
	OriginURL              string            `json:"origin_url"`
	Seed                   string            `json:"seed"`
	Clients                []string          `json:"clients"`
	Drivers                map[string]string `json:"drivers"`
	Metrics                map[string]string `json:"metrics"`
	PeerIPs                map[string]string `json:"peer_ips"`
	States                 []string          `json:"states"`
	Samples                int               `json:"samples"`
	SchedulingPolicy       string            `json:"scheduling_policy"`
	DownloadTimeoutSeconds int               `json:"download_timeout_seconds"`
	WarmupTimeoutSeconds   int               `json:"warmup_timeout_seconds"`
	ReadyTimeoutSeconds    int               `json:"ready_timeout_seconds"`
	OriginLog              string            `json:"origin_log"`
	ManifestPath           string            `json:"manifest_path"`
}

// RunnerReport is <block>/runner.json.
type RunnerReport struct {
	Schema       string          `json:"schema"`
	Status       string          `json:"status"`
	Reasons      []string        `json:"reasons,omitempty"`
	Checks       []results.Check `json:"checks"`
	Samples      int             `json:"samples"`
	Failures     int             `json:"failures"`
	PrepFailures int             `json:"prep_failures"`
	DfgetVersion string          `json:"dfget_version"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   time.Time       `json:"finished_at"`
}

// Runner executes a Plan.
type Runner struct {
	Plan    Plan
	Out     string
	Log     io.Writer
	drivers map[string]*DriverClient
	files   []results.FixtureFile
	origin  *originTail
	samples *os.File
	checks  []results.Check
	report  RunnerReport
}

// Exit codes of `dfbench runner`.
const (
	RunnerOK      = 0
	RunnerError   = 1
	RunnerInvalid = 3
)

// Metric names verified against client v1.5.14 / scheduler v2.5.4 images.
const (
	metricClientTraffic    = "dragonfly_client_download_traffic"
	metricSchedulerTraffic = "dragonfly_scheduler_traffic"
)

func (r *Runner) logf(format string, a ...any) {
	fmt.Fprintf(r.Log, time.Now().UTC().Format("15:04:05.000")+" runner: "+format+"\n", a...)
}

// Run executes the block and returns the runner exit code.
func (r *Runner) Run(ctx context.Context) int {
	r.report = RunnerReport{Schema: results.SchemaVersion, StartedAt: time.Now().UTC()}
	code := r.run(ctx)
	r.report.FinishedAt = time.Now().UTC()
	r.report.Checks = r.checks
	switch {
	case code == RunnerInvalid:
		r.report.Status = results.BlockInvalid
	case code != RunnerOK:
		r.report.Status = results.BlockInfraFail
	default:
		r.report.Status = results.BlockOK
	}
	for _, c := range r.checks {
		if !c.OK && c.Required {
			r.report.Reasons = append(r.report.Reasons, c.Name+": "+c.Detail)
		}
	}
	if err := writeJSON(filepath.Join(r.Out, "runner.json"), r.report); err != nil {
		r.logf("write runner.json: %v", err)
		return RunnerError
	}
	r.logf("done status=%s samples=%d failures=%d", r.report.Status, r.report.Samples, r.report.Failures)
	return code
}

func (r *Runner) check(name string, required, ok bool, detail string, a ...any) bool {
	d := fmt.Sprintf(detail, a...)
	r.checks = append(r.checks, results.Check{Name: name, OK: ok, Required: required, Detail: d})
	r.logf("check %-34s ok=%v %s", name, ok, d)
	return ok
}

func (r *Runner) run(ctx context.Context) int {
	p := r.Plan
	if err := os.MkdirAll(filepath.Join(r.Out, "metrics"), 0o755); err != nil {
		r.logf("mkdir: %v", err)
		return RunnerError
	}
	m, err := ReadManifest(p.ManifestPath)
	if err != nil {
		r.check("fixtures-manifest", true, false, "%v", err)
		return RunnerInvalid
	}
	r.files = m.Files
	r.check("fixtures-manifest", true, true, "%d files, spec %s", len(m.Files), m.SpecHash)

	r.drivers = map[string]*DriverClient{}
	for peer, base := range p.Drivers {
		r.drivers[peer] = &DriverClient{Base: base}
	}
	if !r.waitReady(ctx) {
		return RunnerInvalid
	}
	r.origin = &originTail{path: p.OriginLog}
	if err := r.origin.seekEnd(); err != nil {
		r.check("origin-log", true, false, "%v", err)
		return RunnerInvalid
	}
	f, err := os.Create(filepath.Join(r.Out, "samples.jsonl"))
	if err != nil {
		r.logf("create samples: %v", err)
		return RunnerError
	}
	defer f.Close()
	r.samples = f

	r.snapshot(ctx, "before")
	if !r.warmup(ctx) {
		r.snapshot(ctx, "after")
		return RunnerInvalid
	}
	if err := r.measure(ctx); err != nil {
		r.logf("measure: %v", err)
		r.snapshot(ctx, "after")
		return RunnerError
	}
	r.snapshot(ctx, "after")
	if !r.validate() {
		return RunnerInvalid
	}
	return RunnerOK
}

func (r *Runner) waitReady(ctx context.Context) bool {
	deadline := time.Now().Add(time.Duration(r.Plan.ReadyTimeoutSeconds) * time.Second)
	var last error
	for time.Now().Before(deadline) {
		last = nil
		for _, peer := range sortedKeys(r.drivers) {
			v, err := r.drivers[peer].Get(ctx, "/v1/version")
			if err != nil {
				last = err
				break
			}
			r.report.DfgetVersion = v
		}
		if last == nil {
			for _, name := range sortedKeys(r.Plan.Metrics) {
				if _, err := Scrape(ctx, r.Plan.Metrics[name]); err != nil {
					last = err
					break
				}
			}
		}
		if last == nil {
			return r.check("drivers-and-metrics-ready", true, true, "%d drivers, %d metrics endpoints, %s", len(r.drivers), len(r.Plan.Metrics), r.report.DfgetVersion)
		}
		select {
		case <-ctx.Done():
			return r.check("drivers-and-metrics-ready", true, false, "%v", ctx.Err())
		case <-time.After(time.Second):
		}
	}
	return r.check("drivers-and-metrics-ready", true, false, "%v", last)
}

// snapshot stores raw /metrics of every target under metrics/<phase>/.
func (r *Runner) snapshot(ctx context.Context, phase string) {
	dir := filepath.Join(r.Out, "metrics", phase)
	_ = os.MkdirAll(dir, 0o755)
	for name, url := range r.Plan.Metrics {
		body, err := Scrape(ctx, url)
		if err != nil {
			body = "# scrape error: " + err.Error() + "\n"
		}
		_ = os.WriteFile(filepath.Join(dir, name+".prom"), []byte(body), 0o644)
	}
}

type counters struct {
	peer       results.Traffic
	seedB2S    int64
	normalB2S  int64
	superB2S   int64
	scrapeErrs []string
}

func (r *Runner) counters(ctx context.Context, peer string) counters {
	var c counters
	get := func(name string) []PromSample {
		body, err := Scrape(ctx, r.Plan.Metrics[name])
		if err != nil {
			c.scrapeErrs = append(c.scrapeErrs, err.Error())
			return nil
		}
		return ParseProm(strings.NewReader(body))
	}
	ps := get(peer)
	c.peer = results.Traffic{
		BackToSource: int64(Sum(ps, metricClientTraffic, map[string]string{"type": "BACK_TO_SOURCE"})),
		RemotePeer:   int64(Sum(ps, metricClientTraffic, map[string]string{"type": "REMOTE_PEER"})),
		LocalPeer:    int64(Sum(ps, metricClientTraffic, map[string]string{"type": "LOCAL_PEER"})),
	}
	if peer != r.Plan.Seed {
		c.seedB2S = int64(Sum(get(r.Plan.Seed), metricClientTraffic, map[string]string{"type": "BACK_TO_SOURCE"}))
	} else {
		c.seedB2S = c.peer.BackToSource
	}
	ss := get("scheduler")
	c.normalB2S = int64(Sum(ss, metricSchedulerTraffic, map[string]string{"host_type": "normal", "type": "BACK_TO_SOURCE"}))
	c.superB2S = int64(Sum(ss, metricSchedulerTraffic, map[string]string{"host_type": "super", "type": "BACK_TO_SOURCE"}))
	return c
}

// download runs one dfget on peer and returns a fully attributed sample.
func (r *Runner) download(ctx context.Context, role, state, peer, tag string, idx int, file results.FixtureFile) results.Sample {
	p := r.Plan
	s := results.Sample{
		Run: p.Run, Variant: p.Variant, Block: p.Block, Role: role, State: state, File: file.Name,
		SizeBytes: file.Size, Index: idx, Peer: peer, Tag: tag, ExitCode: -1,
	}
	before := r.counters(ctx, peer)
	_, _ = r.origin.read() // discard anything not caused by this sample
	res, err := r.drivers[peer].Download(ctx, DownloadRequest{
		URL: p.OriginURL + file.Path, Tag: tag, SchedulingPolicy: p.SchedulingPolicy,
		TimeoutSeconds: p.DownloadTimeoutSeconds,
	})
	after := r.counters(ctx, peer)
	reqs, oerr := r.origin.read()
	s.OriginBytes, s.OriginProbes = attributeOrigin(reqs, p.PeerIPs)
	if oerr != nil {
		s.Error = "origin log: " + oerr.Error()
	}
	s.Traffic = results.Traffic{
		BackToSource: after.peer.BackToSource - before.peer.BackToSource,
		RemotePeer:   after.peer.RemotePeer - before.peer.RemotePeer,
		LocalPeer:    after.peer.LocalPeer - before.peer.LocalPeer,
	}
	s.SeedBackToSource = after.seedB2S - before.seedB2S
	s.SchedulerNormalBackToSource = after.normalB2S - before.normalB2S
	s.SchedulerSuperBackToSource = after.superB2S - before.superB2S
	if errs := append(before.scrapeErrs, after.scrapeErrs...); len(errs) > 0 {
		s.MetricsError = strings.Join(errs, "; ")
	}
	if err != nil {
		s.Error = err.Error()
	} else {
		s.ExitCode = res.ExitCode
		s.StartedAt = res.StartedAt
		s.DurationMs = float64(res.DurationNs) / 1e6
		if res.DurationNs > 0 {
			s.MiBps = float64(file.Size) / (1 << 20) / (float64(res.DurationNs) / 1e9)
		}
		s.SHA256OK = res.OK && res.SHA256 == file.SHA256 && res.Bytes == file.Size
		s.OK = res.OK && s.SHA256OK
		if !res.OK {
			s.Error = strings.TrimSpace(res.Error + " " + lastLine(res.Stderr))
		} else if !s.SHA256OK {
			s.Error = fmt.Sprintf("sha256 mismatch: got %s (%d bytes) want %s", res.SHA256, res.Bytes, file.SHA256)
		}
	}
	line, _ := json.Marshal(s)
	r.samples.Write(append(line, '\n'))
	if s.OK {
		r.logf("%-7s %-9s %-11s %-9s #%d %8.1fms %8.1fMiB/s b2s=%d remote=%d local=%d seedb2s=%d", role, state, file.Name, peer, idx, s.DurationMs, s.MiBps, s.Traffic.BackToSource, s.Traffic.RemotePeer, s.Traffic.LocalPeer, s.SeedBackToSource)
	} else {
		r.logf("%-7s %-9s %-11s %-9s #%d FAILED: %s", role, state, file.Name, peer, idx, s.Error)
	}
	return s
}

// warmup proves that the seed peer is announced and used: a client download
// of a fresh task must make the seed (super) go back-to-source while the
// client itself fetches 0 piece bytes from the origin.
func (r *Runner) warmup(ctx context.Context) bool {
	p := r.Plan
	file := r.files[0]
	peer := p.Clients[0]
	deadline := time.Now().Add(time.Duration(p.WarmupTimeoutSeconds) * time.Second)
	var detail string
	for attempt := 1; ; attempt++ {
		tag := fmt.Sprintf("%s-warmup-%d", p.BlockID, attempt)
		s := r.download(ctx, "warmup", results.StateCold, peer, tag, attempt, file)
		switch {
		case !s.OK:
			detail = "warmup download failed: " + s.Error
		case s.Traffic.BackToSource != 0 || s.OriginBytes[peer] != 0:
			detail = fmt.Sprintf("client went back-to-source (metric %d bytes, origin %d bytes): seed peer not announced or not used", s.Traffic.BackToSource, s.OriginBytes[peer])
		case s.SchedulerSuperBackToSource <= 0 && s.SeedBackToSource <= 0:
			detail = "no super seed peer back-to-source traffic observed"
		case s.Traffic.RemotePeer < file.Size:
			detail = fmt.Sprintf("client remote-peer traffic %d < %d", s.Traffic.RemotePeer, file.Size)
		default:
			return r.check("seed-announced", true, true, "attempt %d: seed back-to-source %d bytes, %s remote-peer %d bytes, client back-to-source 0", attempt, s.SeedBackToSource, peer, s.Traffic.RemotePeer)
		}
		r.logf("warmup attempt %d: %s", attempt, detail)
		if time.Now().After(deadline) {
			return r.check("seed-announced", true, false, "after %d attempts: %s", attempt, detail)
		}
		select {
		case <-ctx.Done():
			return r.check("seed-announced", true, false, "%v", ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}

func (r *Runner) measure(ctx context.Context) error {
	p := r.Plan
	want := map[string]bool{}
	for _, s := range p.States {
		want[s] = true
	}
	n := len(p.Clients)
	if n < 2 {
		return errors.New("need at least 2 clients")
	}
	for _, file := range r.files {
		for i := 0; i < p.Samples; i++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a, b := p.Clients[i%n], p.Clients[(i+1)%n]
			tag := func(state string) string {
				return fmt.Sprintf("%s-%s-%s-%d", p.BlockID, state, strings.TrimSuffix(file.Name, ".bin"), i)
			}
			if want[results.StateCold] {
				r.record(r.download(ctx, "measure", results.StateCold, a, tag("cold"), i, file))
			}
			if want[results.StateSeedWarm] {
				t := tag("sw")
				r.record(r.download(ctx, "prep", results.StateSeedWarm, p.Seed, t, i, file))
				r.record(r.download(ctx, "measure", results.StateSeedWarm, b, t, i, file))
			}
			if want[results.StatePeerWarm] || want[results.StateLocalHit] {
				t := tag("pw")
				r.record(r.download(ctx, "prep", results.StatePeerWarm, a, t, i, file))
				r.record(r.download(ctx, "measure", results.StatePeerWarm, b, t, i, file))
				if want[results.StateLocalHit] {
					r.record(r.download(ctx, "measure", results.StateLocalHit, b, t, i, file))
				}
			}
		}
	}
	return nil
}

func (r *Runner) record(s results.Sample) {
	if s.Role == "measure" {
		r.report.Samples++
		if !s.OK {
			r.report.Failures++
		}
	} else if s.Role == "prep" && !s.OK {
		r.report.PrepFailures++
	}
}

// validate re-reads samples.jsonl and applies the block validity rules.
func (r *Runner) validate() bool {
	samples, err := ReadSamples(filepath.Join(r.Out, "samples.jsonl"))
	if err != nil {
		return r.check("samples-readable", true, false, "%v", err)
	}
	var coldN, coldBad, localN, localBad int
	var coldDetail []string
	for _, s := range samples {
		if s.Role != "measure" || !s.OK {
			continue
		}
		switch s.State {
		case results.StateCold:
			coldN++
			if s.Traffic.BackToSource != 0 || s.OriginBytes[s.Peer] != 0 {
				coldBad++
				coldDetail = append(coldDetail, fmt.Sprintf("%s %s b2s=%d origin=%d", s.Peer, s.Tag, s.Traffic.BackToSource, s.OriginBytes[s.Peer]))
			}
		case results.StateLocalHit:
			localN++
			if s.Traffic.LocalPeer < s.SizeBytes || s.Traffic.RemotePeer != 0 {
				localBad++
			}
		}
	}
	ok := r.check("cold-client-back-to-source-zero", true, coldN > 0 && coldBad == 0,
		"%d cold samples, %d with client back-to-source bytes %s", coldN, coldBad, strings.Join(coldDetail, ", "))
	// Informational: local-hit should be served entirely from the local cache.
	r.check("local-hit-served-locally", false, localBad == 0, "%d local-hit samples, %d not fully local", localN, localBad)
	return ok
}

// ReadSamples loads a samples.jsonl file.
func ReadSamples(path string) ([]results.Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []results.Sample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		var s results.Sample
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

// originRequest is one nginx access log line (see origin-nginx.conf).
type originRequest struct {
	Remote    string `json:"remote"`
	URI       string `json:"uri"`
	Status    int    `json:"status"`
	BodyBytes int64  `json:"body_bytes"`
	Range     string `json:"range"`
}

// attributeOrigin sums origin body bytes per peer. dfdaemon issues a
// `Range: bytes=0-0` request to learn the content length before scheduling;
// those 1-byte metadata probes are counted separately, not as piece data.
func attributeOrigin(reqs []originRequest, peerIPs map[string]string) (map[string]int64, map[string]int) {
	byIP := map[string]string{}
	for peer, ip := range peerIPs {
		byIP[ip] = peer
	}
	bytes := map[string]int64{}
	probes := map[string]int{}
	for _, q := range reqs {
		peer := byIP[q.Remote]
		if peer == "" {
			peer = q.Remote
		}
		if q.Range == "bytes=0-0" {
			probes[peer]++
			continue
		}
		bytes[peer] += q.BodyBytes
	}
	return bytes, probes
}

type originTail struct {
	path   string
	offset int64
}

func (o *originTail) seekEnd() error {
	st, err := os.Stat(o.path)
	if errors.Is(err, os.ErrNotExist) {
		o.offset = 0
		return nil
	}
	if err != nil {
		return err
	}
	o.offset = st.Size()
	return nil
}

func (o *originTail) read() ([]originRequest, error) {
	f, err := os.Open(o.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(o.offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// Only consume complete lines.
	end := strings.LastIndexByte(string(data), '\n')
	if end < 0 {
		return nil, nil
	}
	o.offset += int64(end + 1)
	var out []originRequest
	for _, line := range strings.Split(string(data[:end]), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var q originRequest
		if err := json.Unmarshal([]byte(line), &q); err == nil {
			out = append(out, q)
		}
	}
	return out, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// WriteJSON is exported for the orchestrator.
func WriteJSON(path string, v any) error { return writeJSON(path, v) }
