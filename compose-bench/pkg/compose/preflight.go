package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

// MinComposeVersion is required for `up --wait`, service_completed_successfully
// and `--profile '*'`.
var MinComposeVersion = [3]int{2, 24, 0}

// PreflightCheck is one preflight assertion.
type PreflightCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Fatal  bool   `json:"fatal"`
	Detail string `json:"detail"`
}

// Preflight is results/<run>/preflight.json.
type Preflight struct {
	Checks []PreflightCheck `json:"checks"`
	Host   results.Host     `json:"host"`
}

// OK reports whether no fatal check failed.
func (p *Preflight) OK() bool {
	for _, c := range p.Checks {
		if c.Fatal && !c.OK {
			return false
		}
	}
	return true
}

func (p *Preflight) add(name string, fatal, ok bool, format string, a ...any) {
	p.Checks = append(p.Checks, PreflightCheck{Name: name, OK: ok, Fatal: fatal, Detail: fmt.Sprintf(format, a...)})
}

type dockerInfo struct {
	ServerVersion   string `json:"ServerVersion"`
	OperatingSystem string `json:"OperatingSystem"`
	KernelVersion   string `json:"KernelVersion"`
	OSType          string `json:"OSType"`
	Architecture    string `json:"Architecture"`
	NCPU            int    `json:"NCPU"`
	MemTotal        int64  `json:"MemTotal"`
	Driver          string `json:"Driver"`
	DefaultRuntime  string `json:"DefaultRuntime"`
	Name            string `json:"Name"`
}

// EngineArch maps docker's architecture to GOARCH.
func EngineArch(a string) string {
	switch a {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	}
	return a
}

// DockerInfo returns engine information.
func DockerInfo(ctx context.Context, x *Exec) (*dockerInfo, error) {
	out, err := x.Output(ctx, "docker", "info", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var di dockerInfo
	if err := json.Unmarshal([]byte(out), &di); err != nil {
		return nil, err
	}
	if di.ServerVersion == "" {
		return nil, fmt.Errorf("docker engine not reachable")
	}
	return &di, nil
}

// HostInfo collects host and engine facts and classifies the environment.
func HostInfo(ctx context.Context, x *Exec, di *dockerInfo) results.Host {
	h := results.Host{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU()}
	h.Hostname, _ = os.Hostname()
	if out, err := exec.CommandContext(ctx, "uname", "-sr").Output(); err == nil {
		h.Kernel = strings.TrimSpace(string(out))
	}
	h.MemoryBytes = hostMemory(ctx)
	if v, err := x.Output(ctx, "docker", "compose", "version", "--short"); err == nil {
		h.ComposeVersion = strings.TrimPrefix(v, "v")
	}
	if di != nil {
		h.DockerVersion, h.DockerOS, h.DockerKernel = di.ServerVersion, di.OperatingSystem, di.KernelVersion
		h.DockerCPUs, h.DockerMemory, h.StorageDriver, h.Runtime = di.NCPU, di.MemTotal, di.Driver, di.DefaultRuntime
	}
	osName := strings.ToLower(h.DockerOS)
	switch {
	case strings.Contains(osName, "docker desktop"):
		h.Classification = "docker-desktop"
	case strings.Contains(osName, "orbstack"):
		h.Classification = "orbstack"
	case runtime.GOOS != "linux":
		h.Classification = "vm-backed"
	default:
		h.Classification = "linux-native"
	}
	h.BaselineEligible = h.Classification == "linux-native"
	if h.BaselineEligible {
		h.Note = "native Linux engine; eligible for formal baselines when the host is fixed"
	} else {
		h.Note = fmt.Sprintf("%s engine on %s (VM-backed, Docker-Desktop-class); results are non-baseline functional smoke data", h.Classification, runtime.GOOS)
	}
	return h
}

func hostMemory(ctx context.Context) int64 {
	if runtime.GOOS == "darwin" {
		if out, err := exec.CommandContext(ctx, "sysctl", "-n", "hw.memsize").Output(); err == nil {
			n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
			return n
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 && f[0] == "MemTotal:" {
				n, _ := strconv.ParseInt(f[1], 10, 64)
				return n * 1024
			}
		}
	}
	return 0
}

// ParseVersion parses "2.24.1" / "v5.1.2" / "2.24.1-desktop.1".
func ParseVersion(s string) ([3]int, error) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return v, fmt.Errorf("bad version %q", s)
	}
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return v, fmt.Errorf("bad version %q", s)
		}
		v[i] = n
	}
	return v, nil
}

func versionLess(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// PreflightOptions configures RunPreflight.
type PreflightOptions struct {
	ResultsDir   string
	NetPrefix    string
	MinFreeGiB   float64
	DiskProbeImg string // small local image used to measure engine disk space
}

// RunPreflight checks the compose version, disk space, subnet conflicts and
// classifies the platform.
func RunPreflight(ctx context.Context, x *Exec, di *dockerInfo, o PreflightOptions) *Preflight {
	p := &Preflight{Host: HostInfo(ctx, x, di)}
	p.add("docker-engine", true, di != nil, "docker %s on %s (%s)", p.Host.DockerVersion, p.Host.DockerOS, p.Host.DockerKernel)

	cv, err := ParseVersion(p.Host.ComposeVersion)
	p.add("compose-version", true, err == nil && !versionLess(cv, MinComposeVersion),
		"docker compose %s (need >= %d.%d.%d)", p.Host.ComposeVersion, MinComposeVersion[0], MinComposeVersion[1], MinComposeVersion[2])

	var st syscall.Statfs_t
	if err := syscall.Statfs(o.ResultsDir, &st); err == nil {
		free := float64(st.Bavail) * float64(st.Bsize) / (1 << 30)
		p.add("disk-results", true, free >= o.MinFreeGiB, "%.1f GiB free at %s (need %.1f)", free, o.ResultsDir, o.MinFreeGiB)
	} else {
		p.add("disk-results", true, false, "statfs %s: %v", o.ResultsDir, err)
	}
	if o.DiskProbeImg != "" {
		out, err := x.Output(ctx, "docker", "run", "--rm", "--network", "none", "--entrypoint", "df", o.DiskProbeImg, "-Pk", "/")
		free := -1.0
		if err == nil {
			lines := strings.Split(out, "\n")
			if f := strings.Fields(lines[len(lines)-1]); len(f) >= 4 {
				if kb, perr := strconv.ParseFloat(f[3], 64); perr == nil {
					free = kb / (1 << 20)
				}
			}
		}
		p.add("disk-engine", true, free >= o.MinFreeGiB, "%.1f GiB free in the docker engine filesystem (need %.1f) %v", free, o.MinFreeGiB, errOrEmpty(err))
	}

	conflicts, err := subnetConflicts(ctx, x, o.NetPrefix+".0/24")
	if err != nil {
		p.add("subnet", true, false, "%v", err)
	} else {
		p.add("subnet", true, len(conflicts) == 0, "%s.0/24 conflicts: %v", o.NetPrefix, conflicts)
	}
	stale, _ := x.Output(ctx, "docker", "ps", "-a", "--filter", "label=dfperf.run", "--format", "{{.Label \"com.docker.compose.project\"}}")
	p.add("no-stale-dfperf-projects", true, strings.TrimSpace(stale) == "", "stale dfperf containers: %q (run `compose-bench.sh clean`)", strings.TrimSpace(stale))
	p.add("platform", false, p.Host.BaselineEligible, "%s: %s", p.Host.Classification, p.Host.Note)
	return p
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func subnetConflicts(ctx context.Context, x *Exec, cidr string) ([]string, error) {
	_, want, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	ids, err := x.Output(ctx, "docker", "network", "ls", "-q")
	if err != nil {
		return nil, err
	}
	var conflicts []string
	for _, id := range strings.Fields(ids) {
		out, err := x.Output(ctx, "docker", "network", "inspect", "--format", "{{.Name}}{{range .IPAM.Config}} {{.Subnet}}{{end}}", id)
		if err != nil {
			continue
		}
		f := strings.Fields(out)
		for _, s := range f[1:] {
			_, n, err := net.ParseCIDR(s)
			if err != nil {
				continue
			}
			if n.Contains(want.IP) || want.Contains(n.IP) {
				conflicts = append(conflicts, f[0]+"="+s)
			}
		}
	}
	// Host interfaces (VPNs etc.) on Linux hosts.
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && (want.Contains(ipn.IP) || ipn.Contains(want.IP)) {
				conflicts = append(conflicts, "host-interface="+ipn.String())
			}
		}
	}
	return conflicts, nil
}
