package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseRef(t *testing.T) {
	cases := []struct{ in, s, c string }{
		{"v2.5.4+v1.5.14", "dragonflyoss/scheduler:v2.5.4", "dragonflyoss/client:v1.5.14"},
		{"latest", "dragonflyoss/scheduler:latest", "dragonflyoss/client:latest"},
		{"scheduler=reg/s@sha256:aa,client=reg/c:v1", "reg/s@sha256:aa", "reg/c:v1"},
		{"my/sched:x+my/client:y", "my/sched:x", "my/client:y"},
	}
	for _, c := range cases {
		s, cl, err := ParseRef(c.in)
		if err != nil || s != c.s || cl != c.c {
			t.Errorf("ParseRef(%q) = %q %q %v", c.in, s, cl, err)
		}
	}
	for _, bad := range []string{"", "scheduler=x", "foo=bar,client=y"} {
		if _, _, err := ParseRef(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRepoOf(t *testing.T) {
	for in, want := range map[string]string{
		"dragonflyoss/client:v1": "dragonflyoss/client", "dragonflyoss/client@sha256:ab": "dragonflyoss/client",
		"localhost:5000/x/y:z": "localhost:5000/x/y", "nginx": "nginx",
	} {
		if got := repoOf(in); got != want {
			t.Errorf("repoOf(%q) = %q", in, got)
		}
	}
	if normalizeRepo("docker.io/library/nginx") != "nginx" {
		t.Fatal("normalize")
	}
}

func TestParseVersion(t *testing.T) {
	v, err := ParseVersion("v5.1.2")
	if err != nil || v != [3]int{5, 1, 2} || versionLess(v, MinComposeVersion) {
		t.Fatalf("%v %v", v, err)
	}
	v, _ = ParseVersion("2.20.3-desktop.1")
	if !versionLess(v, MinComposeVersion) {
		t.Fatal("2.20 should be too old")
	}
	if _, err := ParseVersion("garbage"); err == nil {
		t.Fatal("accepted garbage")
	}
}

func TestRenderConfigsAndOverlays(t *testing.T) {
	root := "../../compose/config"
	out := t.TempDir()
	topo := Topology{NetPrefix: "10.213.0"}
	if _, err := RenderConfigs(root, filepath.Join(root, "overlays"), out, topo, nil); err != nil {
		t.Fatal(err)
	}
	read := func(n string) map[string]any {
		data, err := os.ReadFile(filepath.Join(out, n))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := yaml.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	seed := read("seed-client.yaml")
	if seed["host"].(map[string]any)["ip"] != "10.213.0.20" || seed["seedPeer"].(map[string]any)["type"] != "super" || seed["seedPeer"].(map[string]any)["enable"] != true {
		t.Fatalf("seed config %v", seed["seedPeer"])
	}
	c2 := read("client-2.yaml")
	if c2["host"].(map[string]any)["ip"] != "10.213.0.22" || c2["seedPeer"].(map[string]any)["enable"] != false {
		t.Fatalf("client-2 %v", c2["host"])
	}
	sched := read("scheduler.yaml")
	if _, ok := sched["manager"]; ok {
		t.Fatal("scheduler must run without manager")
	}
	if sched["server"].(map[string]any)["advertiseIP"] != "10.213.0.10" {
		t.Fatal("scheduler advertiseIP")
	}
	dyn := read("client-1.dynconfig.yaml")
	if addrs := dyn["scheduler"].(map[string]any)["addrs"].([]any); addrs[0] != "10.213.0.10:8002" {
		t.Fatalf("dynconfig %v", addrs)
	}
	for _, f := range []string{"client-1.yaml", "seed-client.yaml"} {
		if gc := read(f)["gc"].(map[string]any); gc["interval"] != "3600s" {
			t.Fatalf("gc not fixed in %s: %v", f, gc)
		}
	}

	out2 := t.TempDir()
	applied, err := RenderConfigs(root, filepath.Join(root, "overlays"), out2, topo, []string{"broken-seed", "seed-wrong-scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied %v", applied)
	}
	data, _ := os.ReadFile(filepath.Join(out2, "seed-client.yaml"))
	var m map[string]any
	yaml.Unmarshal(data, &m)
	if m["seedPeer"].(map[string]any)["enable"] != false || m["host"].(map[string]any)["ip"] != "10.213.0.20" {
		t.Fatalf("overlay merge %v", m)
	}
	data, _ = os.ReadFile(filepath.Join(out2, "seed-client.dynconfig.yaml"))
	if !strings.Contains(string(data), "10.213.0.99:8002") {
		t.Fatalf("wrong-scheduler overlay: %s", data)
	}
	data, _ = os.ReadFile(filepath.Join(out2, "client-1.dynconfig.yaml"))
	if strings.Contains(string(data), ".99:") {
		t.Fatal("overlay leaked into client-1")
	}
	if _, err := RenderConfigs(root, filepath.Join(root, "overlays"), t.TempDir(), topo, []string{"does-not-exist"}); err == nil {
		t.Fatal("unknown overlay accepted")
	}
}

func TestMergeYAML(t *testing.T) {
	out, err := MergeYAML("a: {b: 1, c: [1, 2]}\nd: x\n", "a: {c: [3]}\ne: y\n")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	yaml.Unmarshal([]byte(out), &m)
	a := m["a"].(map[string]any)
	if a["b"] != 1 || len(a["c"].([]any)) != 1 || m["d"] != "x" || m["e"] != "y" {
		t.Fatalf("merged %v", m)
	}
}

func TestLoadProfile(t *testing.T) {
	p, err := LoadProfile("../../compose/profiles/file-3peers.env")
	if err != nil {
		t.Fatal(err)
	}
	if p.Int("DFPERF_BLOCKS", 0) < 4 {
		t.Fatal("profile must default to >= 4 blocks per variant")
	}
	if p.Str("DFPERF_SCHEDULING_POLICY", "") != "always" {
		t.Fatal("scheduling policy must be always")
	}
	if p.Str("DFPERF_NOPE", "d") != "d" {
		t.Fatal("default")
	}
}

func TestComposeFileHardRules(t *testing.T) {
	data, err := os.ReadFile("../../compose/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
		Volumes  map[string]map[string]any `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "docker.sock") && !strings.Contains(text, "no docker.sock anywhere") {
		t.Fatal("docker.sock mounted")
	}
	for name, svc := range doc.Services {
		if _, ok := svc["ports"]; ok {
			t.Errorf("%s publishes host ports", name)
		}
		if _, ok := svc["container_name"]; ok {
			t.Errorf("%s has a fixed container_name", name)
		}
		if svc["restart"] != "no" {
			t.Errorf("%s restart = %v", name, svc["restart"])
		}
		if vols, ok := svc["volumes"].([]any); ok {
			for _, v := range vols {
				if strings.Contains(v.(string), "docker.sock") {
					t.Errorf("%s mounts docker.sock", name)
				}
			}
		}
	}
	for _, want := range []string{"scheduler", "seed-client", "client-1", "client-2", "client-3", "driver-seed", "driver-1", "driver-2", "driver-3", "origin", "fixtures", "runner", "compare"} {
		if _, ok := doc.Services[want]; !ok {
			t.Errorf("service %s missing", want)
		}
	}
	if doc.Volumes["fixtures"]["external"] != true {
		t.Fatal("fixtures volume must be external")
	}
	for _, tool := range []string{"runner", "compare"} {
		if p, _ := doc.Services[tool]["profiles"].([]any); len(p) != 1 || p[0] != "tools" {
			t.Errorf("%s must be in the tools profile", tool)
		}
	}
}
