package compose

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Topology is the fixed MVP layout: one scheduler, one super seed peer and
// three normal peers, each with a fixed IP in a /24.
type Topology struct {
	NetPrefix string // e.g. 10.213.0
}

// Peers returns peer name -> last octet.
func (t Topology) Peers() map[string]int {
	return map[string]int{"seed-client": 20, "client-1": 21, "client-2": 22, "client-3": 23}
}

// IP returns the IP of a service.
func (t Topology) IP(service string) string {
	octets := map[string]int{"origin": 5, "scheduler": 10, "driver-seed": 30, "driver-1": 31, "driver-2": 32, "driver-3": 33, "runner": 40}
	for k, v := range t.Peers() {
		octets[k] = v
	}
	return fmt.Sprintf("%s.%d", t.NetPrefix, octets[service])
}

// Clients lists the normal peers.
func (t Topology) Clients() []string { return []string{"client-1", "client-2", "client-3"} }

// RenderConfigs renders compose/config templates for one variant into
// outDir and applies overlays (compose/config/overlays/<name>/<file>.yaml,
// deep-merged into the rendered file of the same name).
func RenderConfigs(templateDir, overlayRoot, outDir string, topo Topology, overlays []string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	read := func(name string) (string, error) {
		b, err := os.ReadFile(filepath.Join(templateDir, name))
		return string(b), err
	}
	files := map[string]string{}
	sched := topo.IP("scheduler")
	tpl, err := read("scheduler.yaml")
	if err != nil {
		return nil, err
	}
	files["scheduler.yaml"] = strings.ReplaceAll(tpl, "__SCHEDULER_IP__", sched)
	if files["scheduler-dynconfig.yaml"], err = read("scheduler-dynconfig.yaml"); err != nil {
		return nil, err
	}
	if files["origin-nginx.conf"], err = read("origin-nginx.conf"); err != nil {
		return nil, err
	}
	dfd, err := read("dfdaemon.yaml")
	if err != nil {
		return nil, err
	}
	dyn, err := read("dynconfig.yaml")
	if err != nil {
		return nil, err
	}
	for peer := range topo.Peers() {
		enable, typ := "false", "normal"
		if peer == "seed-client" {
			enable, typ = "true", "super"
		}
		r := strings.NewReplacer("__HOST_IP__", topo.IP(peer), "__HOSTNAME__", peer,
			"__SEED_PEER_ENABLE__", enable, "__SEED_PEER_TYPE__", typ)
		files[peer+".yaml"] = r.Replace(dfd)
		files[peer+".dynconfig.yaml"] = strings.ReplaceAll(dyn, "__SCHEDULER_IP__", sched)
	}
	var applied []string
	for _, ov := range overlays {
		dir := filepath.Join(overlayRoot, ov)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("overlay %s: %w", ov, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			base, ok := files[e.Name()]
			if !ok {
				return nil, fmt.Errorf("overlay %s: %s does not match a rendered config file", ov, e.Name())
			}
			patch, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			patched := strings.NewReplacer("__SCHEDULER_IP__", sched, "__NET_PREFIX__", topo.NetPrefix).Replace(string(patch))
			merged, err := MergeYAML(base, patched)
			if err != nil {
				return nil, fmt.Errorf("overlay %s/%s: %w", ov, e.Name(), err)
			}
			files[e.Name()] = fmt.Sprintf("# rendered with overlay %q\n%s", ov, merged)
			applied = append(applied, ov+"/"+e.Name())
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(outDir, n), []byte(files[n]), 0o644); err != nil {
			return nil, err
		}
	}
	// Validate every YAML we produced parses.
	for _, n := range names {
		if strings.HasSuffix(n, ".yaml") {
			var v any
			if err := yaml.Unmarshal([]byte(files[n]), &v); err != nil {
				return nil, fmt.Errorf("rendered %s is not valid YAML: %w", n, err)
			}
		}
	}
	return applied, nil
}

// MergeYAML deep-merges patch into base (maps merge recursively, everything
// else is replaced) and returns the YAML text.
func MergeYAML(base, patch string) (string, error) {
	var b, p map[string]any
	if err := yaml.Unmarshal([]byte(base), &b); err != nil {
		return "", err
	}
	if err := yaml.Unmarshal([]byte(patch), &p); err != nil {
		return "", err
	}
	if b == nil {
		b = map[string]any{}
	}
	merged := mergeMaps(b, p)
	out, err := yaml.Marshal(merged)
	return string(out), err
}

func mergeMaps(dst, src map[string]any) map[string]any {
	for k, sv := range src {
		if sm, ok := sv.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				dst[k] = mergeMaps(dm, sm)
				continue
			}
		}
		dst[k] = sv
	}
	return dst
}

// tarDir writes the contents of dir (relative paths) into a tar file.
func tarDir(dir, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(f)
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, src)
			src.Close()
			return err
		}
		return nil
	})
	if cerr := tw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
