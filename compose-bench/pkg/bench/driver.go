package bench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// DownloadRequest asks a driver to run one dfget download.
type DownloadRequest struct {
	URL              string `json:"url"`
	Tag              string `json:"tag"`
	SchedulingPolicy string `json:"scheduling_policy"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	// Keep leaves the output file in place (default: removed after hashing).
	Keep bool `json:"keep"`
}

// DownloadResult is the driver's answer.
type DownloadResult struct {
	OK         bool      `json:"ok"`
	ExitCode   int       `json:"exit_code"`
	Error      string    `json:"error,omitempty"`
	DurationNs int64     `json:"duration_ns"`
	Bytes      int64     `json:"bytes"`
	SHA256     string    `json:"sha256"`
	StartedAt  time.Time `json:"started_at"`
	Stderr     string    `json:"stderr,omitempty"`
	Args       []string  `json:"args"`
}

// Driver runs dfget against the dfdaemon unix socket it shares with the
// daemon container. It is exposed over HTTP because the runner has no
// docker.sock and therefore cannot `docker exec`.
type Driver struct {
	DfgetPath string
	Endpoint  string
	Workdir   string
	seq       atomic.Int64
}

var safeTag = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Handler returns the driver HTTP API.
func (d *Driver) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(d.Endpoint); err != nil {
			http.Error(w, "dfdaemon socket missing: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		out, err := exec.CommandContext(r.Context(), d.DfgetPath, "--version").CombinedOutput()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(bytes.TrimSpace(out))
	})
	mux.HandleFunc("/v1/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req DownloadRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res := d.Download(r.Context(), req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
	return mux
}

// Args builds the dfget command line. Verified against dfget 1.5.14:
//   - --endpoint is the dfdaemon unix socket path;
//   - --transfer-from-dfdaemon is a bare boolean flag (passing "=false" is a
//     clap error), so the default (dfdaemon hard-links/copies the file to the
//     output path) is used by not passing it; the output therefore has to be
//     on a volume shared with the daemon;
//   - --scheduling-policy=always is required, otherwise "auto" downloads small
//     files directly from the source and bypasses the seed peer.
func (d *Driver) Args(req DownloadRequest, output string) []string {
	policy := req.SchedulingPolicy
	if policy == "" {
		policy = "always"
	}
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = 600
	}
	return []string{
		req.URL,
		"--output", output,
		"--endpoint", d.Endpoint,
		"--tag", req.Tag,
		"--scheduling-policy", policy,
		"--timeout", fmt.Sprintf("%ds", timeout),
		"--no-progress",
		"--log-level", "warn",
	}
}

// Download runs a single dfget and hashes the result outside the timed region.
func (d *Driver) Download(ctx context.Context, req DownloadRequest) DownloadResult {
	res := DownloadResult{ExitCode: -1}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		res.Error = "url must be http(s)"
		return res
	}
	if !safeTag.MatchString(req.Tag) {
		res.Error = "invalid tag"
		return res
	}
	if err := os.MkdirAll(d.Workdir, 0o755); err != nil {
		res.Error = err.Error()
		return res
	}
	output := filepath.Join(d.Workdir, fmt.Sprintf("%s-%d.out", req.Tag, d.seq.Add(1)))
	_ = os.Remove(output)
	args := d.Args(req, output)
	res.Args = args
	timeout := time.Duration(req.TimeoutSeconds+30) * time.Second
	if req.TimeoutSeconds <= 0 {
		timeout = 630 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, d.DfgetPath, args...)
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	res.StartedAt = time.Now().UTC()
	start := time.Now()
	err := cmd.Run()
	res.DurationNs = time.Since(start).Nanoseconds()
	res.Stderr = tail(stderr.String(), 4096)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		}
		res.Error = "dfget: " + err.Error()
		_ = os.Remove(output)
		return res
	}
	res.ExitCode = 0
	sum, n, err := hashFile(output)
	if !req.Keep {
		_ = os.Remove(output)
	}
	if err != nil {
		res.Error = "hash output: " + err.Error()
		return res
	}
	res.SHA256, res.Bytes, res.OK = sum, n, true
	return res
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// DriverClient calls a driver over HTTP.
type DriverClient struct {
	Base string
	HTTP *http.Client
}

// Download posts a download request and decodes the result.
func (c *DriverClient) Download(ctx context.Context, req DownloadRequest) (DownloadResult, error) {
	body, _ := json.Marshal(req)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/v1/download", bytes.NewReader(body))
	if err != nil {
		return DownloadResult{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(hreq)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return DownloadResult{}, fmt.Errorf("driver %s: %s: %s", c.Base, resp.Status, strings.TrimSpace(string(b)))
	}
	var res DownloadResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return DownloadResult{}, err
	}
	return res, nil
}

// Get fetches a small text endpoint (healthz, version).
func (c *DriverClient) Get(ctx context.Context, path string) (string, error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.client().Do(hreq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s%s: %s: %s", c.Base, path, resp.Status, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

func (c *DriverClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Minute}
}
