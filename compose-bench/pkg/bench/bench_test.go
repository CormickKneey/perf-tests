package bench

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSizes(t *testing.T) {
	got, err := ParseSizes("64MiB, 4MiB,4194304,1GiB")
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{4 << 20, 64 << 20, 1 << 30}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
	for _, bad := range []string{"", "x", "-1MiB", "0"} {
		if _, err := ParseSizes(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if FormatSize(64<<20) != "64MiB" || FixtureName(4<<20) != "f-4mib.bin" {
		t.Fatal("format")
	}
}

func TestFixturesDeterministicAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	sizes := []int64{1000, 1 << 20}
	m1, err := GenerateFixtures(dir, "seed", sizes, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Files[1].SHA256 != ExpectedSHA256("seed", 1<<20) {
		t.Fatal("manifest digest differs from expected digest")
	}
	st, _ := os.Stat(filepath.Join(dir, "files", m1.Files[0].Name))
	m2, err := GenerateFixtures(dir, "seed", sizes, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(filepath.Join(dir, "files", m1.Files[0].Name))
	if !st.ModTime().Equal(st2.ModTime()) || m2.SpecHash != m1.SpecHash {
		t.Fatal("second generation rewrote files")
	}
	if ExpectedSHA256("other", 1<<20) == m1.Files[1].SHA256 {
		t.Fatal("seed does not change content")
	}
	if v := FixtureVolumeName("seed", sizes); !strings.HasPrefix(v, "dfperf-fixtures-") || len(v) != len("dfperf-fixtures-")+16 {
		t.Fatalf("volume name %s", v)
	}
	sums, _ := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if !strings.Contains(string(sums), m1.Files[0].SHA256+"  files/") {
		t.Fatal("SHA256SUMS")
	}
}

func TestParseProm(t *testing.T) {
	text := `# HELP dragonfly_client_download_traffic x
# TYPE dragonfly_client_download_traffic counter
dragonfly_client_download_traffic{type="REMOTE_PEER"} 16777216
dragonfly_client_download_traffic{type="LOCAL_PEER"} 1.6777216e+07
dragonfly_scheduler_traffic{host_type="super",task_type="STANDARD",type="BACK_TO_SOURCE"} 3.3554432e+07
dragonfly_scheduler_traffic{host_type="normal",task_type="STANDARD",type="REMOTE_PEER"} 5
plain_metric 3
weird{a="x\"y,z",b="2"} 7
`
	s := ParseProm(strings.NewReader(text))
	if v := Sum(s, "dragonfly_client_download_traffic", map[string]string{"type": "REMOTE_PEER"}); v != 16777216 {
		t.Fatalf("remote %v", v)
	}
	if v := Sum(s, "dragonfly_client_download_traffic", nil); v != 2*16777216 {
		t.Fatalf("total %v", v)
	}
	if v := Sum(s, "dragonfly_scheduler_traffic", map[string]string{"host_type": "super", "type": "back_to_source"}); v != 33554432 {
		t.Fatalf("super b2s %v", v)
	}
	if v := Sum(s, "dragonfly_client_download_traffic", map[string]string{"type": "BACK_TO_SOURCE"}); v != 0 {
		t.Fatalf("absent series %v", v)
	}
	if v := Sum(s, "weird", map[string]string{"a": `x"y,z`, "b": "2"}); v != 7 {
		t.Fatalf("escaped labels %v", v)
	}
	if Sum(s, "plain_metric", nil) != 3 {
		t.Fatal("plain")
	}
}

func TestAttributeOrigin(t *testing.T) {
	peers := map[string]string{"seed-client": "10.0.0.20", "client-1": "10.0.0.21"}
	reqs := []originRequest{
		{Remote: "10.0.0.21", BodyBytes: 1, Range: "bytes=0-0"},
		{Remote: "10.0.0.20", BodyBytes: 1, Range: "bytes=0-0"},
		{Remote: "10.0.0.20", BodyBytes: 4194304, Range: "bytes=0-4194303"},
		{Remote: "10.9.9.9", BodyBytes: 10},
	}
	bytes, probes := attributeOrigin(reqs, peers)
	if bytes["client-1"] != 0 || bytes["seed-client"] != 4194304 || bytes["10.9.9.9"] != 10 {
		t.Fatalf("bytes %v", bytes)
	}
	if probes["client-1"] != 1 || probes["seed-client"] != 1 {
		t.Fatalf("probes %v", probes)
	}
}

func TestOriginTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.jsonl")
	o := &originTail{path: path}
	if err := o.seekEnd(); err != nil {
		t.Fatal(err)
	}
	if r, err := o.read(); err != nil || len(r) != 0 {
		t.Fatalf("missing file: %v %v", r, err)
	}
	os.WriteFile(path, []byte(`{"remote":"a","body_bytes":5}`+"\n"+`{"remote":"b","body_bytes":6}`+"\n"+`{"remote":"partial"`), 0o644)
	r, err := o.read()
	if err != nil || len(r) != 2 || r[1].BodyBytes != 6 {
		t.Fatalf("read %v %v", r, err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`,"body_bytes":7}` + "\n")
	f.Close()
	r, _ = o.read()
	if len(r) != 1 || r[0].Remote != "partial" || r[0].BodyBytes != 7 {
		t.Fatalf("partial line handling %v", r)
	}
}

func TestDriverArgsVerifiedFlags(t *testing.T) {
	d := &Driver{Endpoint: "/var/run/dragonfly/dfdaemon.sock"}
	args := strings.Join(d.Args(DownloadRequest{URL: "http://o/f", Tag: "t"}, "/out"), " ")
	for _, want := range []string{"--endpoint /var/run/dragonfly/dfdaemon.sock", "--scheduling-policy always", "--tag t", "--output /out"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "transfer-from-dfdaemon") {
		t.Fatal("--transfer-from-dfdaemon must not be passed (bare bool flag, default false)")
	}
}

// TestDriverDownloadWithFakeDfget runs the HTTP driver against a shell script
// standing in for dfget.
func TestDriverDownloadWithFakeDfget(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "dfget")
	script := "#!/bin/sh\nout=''\nwhile [ $# -gt 0 ]; do case \"$1\" in --output) out=\"$2\"; shift;; esac; shift; done\n" +
		"case \"$out\" in *fail*) echo boom >&2; exit 7;; esac\nprintf hello > \"$out\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "dfdaemon.sock")
	os.WriteFile(sock, nil, 0o644)
	d := &Driver{DfgetPath: fake, Endpoint: sock, Workdir: filepath.Join(dir, "out")}
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()
	c := &DriverClient{Base: srv.URL}
	if _, err := c.Get(context.Background(), "/v1/healthz"); err != nil {
		t.Fatal(err)
	}
	res, err := c.Download(context.Background(), DownloadRequest{URL: "http://origin/f", Tag: "ok-1"})
	if err != nil || !res.OK || res.Bytes != 5 || res.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("download %+v %v", res, err)
	}
	if entries, _ := os.ReadDir(d.Workdir); len(entries) != 0 {
		t.Fatal("output not removed")
	}
	res, err = c.Download(context.Background(), DownloadRequest{URL: "http://origin/f", Tag: "fail-1"})
	if err != nil || res.OK || res.ExitCode != 7 || !strings.Contains(res.Stderr, "boom") {
		t.Fatalf("failure %+v %v", res, err)
	}
	res, _ = c.Download(context.Background(), DownloadRequest{URL: "file:///etc/passwd", Tag: "x"})
	if res.OK || res.Error == "" {
		t.Fatal("non-http url accepted")
	}
	res, _ = c.Download(context.Background(), DownloadRequest{URL: "http://o/f", Tag: "../../etc"})
	if res.OK {
		t.Fatal("unsafe tag accepted")
	}
}
