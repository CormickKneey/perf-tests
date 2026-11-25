package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

// Images resolves refs to local, digest-pinned images.
type Images struct {
	X *Exec
	// Arch is the docker engine architecture (amd64/arm64).
	Arch string
	// CranePath enables the host-side pull fallback (crane pull + docker
	// load) when the daemon cannot pull (e.g. a broken registry mirror).
	CranePath string
	// WorkDir holds the crane OCI layouts and an empty DOCKER_CONFIG so no
	// registry credentials are ever read.
	WorkDir string
}

type inspect struct {
	ID           string   `json:"Id"`
	RepoTags     []string `json:"RepoTags"`
	RepoDigests  []string `json:"RepoDigests"`
	Architecture string   `json:"Architecture"`
	Os           string   `json:"Os"`
	Config       struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

func (im *Images) inspect(ctx context.Context, ref string) (*inspect, error) {
	out, err := im.X.Output(ctx, "docker", "image", "inspect", ref)
	if err != nil {
		return nil, err
	}
	var arr []inspect
	if err := json.Unmarshal([]byte(out), &arr); err != nil || len(arr) == 0 {
		return nil, fmt.Errorf("inspect %s: %v", ref, err)
	}
	return &arr[0], nil
}

// repoOf returns the repository part of an image ref (no tag or digest).
func repoOf(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	if c := strings.LastIndex(ref, ":"); c > slash {
		ref = ref[:c]
	}
	return ref
}

func normalizeRepo(r string) string {
	r = strings.TrimPrefix(r, "docker.io/")
	r = strings.TrimPrefix(r, "library/")
	return r
}

// Resolve makes ref available locally and returns its pinned reference.
func (im *Images) Resolve(ctx context.Context, component, ref string) (results.Image, error) {
	img := results.Image{Component: component, Ref: ref, Platform: "linux/" + im.Arch}
	ins, err := im.inspect(ctx, ref)
	img.Source = "local"
	if err != nil {
		im.X.logf("image %s not present locally, pulling", ref)
		if _, perr := im.X.Output(ctx, "docker", "pull", "--platform", img.Platform, ref); perr == nil {
			img.Source = "docker-pull"
		} else if im.CranePath != "" {
			im.X.logf("docker pull failed (%v); falling back to crane pull + docker load", firstLine(perr.Error()))
			if cerr := im.cranePull(ctx, ref); cerr != nil {
				return img, fmt.Errorf("resolve %s: docker pull: %v; crane: %w", ref, perr, cerr)
			}
			img.Source = "crane-load"
		} else {
			return img, fmt.Errorf("resolve %s: %w", ref, perr)
		}
		if ins, err = im.inspect(ctx, ref); err != nil {
			return img, err
		}
	}
	if ins.Architecture != im.Arch {
		return img, fmt.Errorf("image %s is %s, engine is %s", ref, ins.Architecture, im.Arch)
	}
	img.ImageID = ins.ID
	img.RepoDigests = ins.RepoDigests
	repo := normalizeRepo(repoOf(ref))
	for _, d := range ins.RepoDigests {
		if normalizeRepo(repoOf(d)) == repo {
			img.Pinned = d
			break
		}
	}
	if img.Pinned == "" && len(ins.RepoDigests) > 0 {
		img.Pinned = ins.RepoDigests[0]
	}
	if img.Pinned == "" {
		return img, fmt.Errorf("image %s has no repo digest; refusing to run an unpinned image", ref)
	}
	if _, err := im.inspect(ctx, img.Pinned); err != nil {
		return img, fmt.Errorf("pinned ref %s not resolvable locally: %w", img.Pinned, err)
	}
	if strings.Contains(ref, "@sha256:") {
		img.RegistryDigest = ref[strings.Index(ref, "@")+1:]
	} else if im.CranePath != "" {
		if d, err := im.X.OutputEnv(ctx, im.craneEnv(), im.CranePath, "digest", ref); err == nil {
			img.RegistryDigest = d
		}
	}
	return img, nil
}

func (im *Images) craneEnv() []string {
	cfg := filepath.Join(im.WorkDir, "docker-config")
	_ = os.MkdirAll(cfg, 0o700)
	return []string{"DOCKER_CONFIG=" + cfg}
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// cranePull downloads ref for the engine platform as an OCI layout, names it
// and loads it into the engine (containerd image store keeps the manifest
// digest, so repo@sha256 resolves locally afterwards).
func (im *Images) cranePull(ctx context.Context, ref string) error {
	name := unsafeChars.ReplaceAllString(ref, "_")
	dir := filepath.Join(im.WorkDir, "images", name)
	_ = os.RemoveAll(dir)
	if _, err := im.X.OutputEnv(ctx, im.craneEnv(), im.CranePath, "pull", "--format=oci", "--platform", "linux/"+im.Arch, ref, dir); err != nil {
		return err
	}
	idxPath := filepath.Join(dir, "index.json")
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return err
	}
	var idx map[string]any
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	full := ref
	if !strings.Contains(strings.SplitN(full, "/", 2)[0], ".") {
		if !strings.Contains(full, "/") {
			full = "library/" + full
		}
		full = "docker.io/" + full
	}
	if !strings.Contains(full, "@") && !strings.Contains(full[strings.LastIndex(full, "/"):], ":") {
		full += ":latest"
	}
	mans, _ := idx["manifests"].([]any)
	if len(mans) != 1 {
		return fmt.Errorf("crane layout has %d manifests", len(mans))
	}
	m := mans[0].(map[string]any)
	ann, _ := m["annotations"].(map[string]any)
	if ann == nil {
		ann = map[string]any{}
	}
	ann["io.containerd.image.name"] = full
	m["annotations"] = ann
	out, _ := json.Marshal(idx)
	if err := os.WriteFile(idxPath, out, 0o644); err != nil {
		return err
	}
	tarPath := dir + ".tar"
	if err := tarDir(dir, tarPath); err != nil {
		return err
	}
	defer os.Remove(tarPath)
	_, err = im.X.Output(ctx, "docker", "load", "-i", tarPath)
	return err
}

// Version asks the binary inside the image for its version string.
func (im *Images) Version(ctx context.Context, component, pinned string) string {
	var out string
	var err error
	switch component {
	case "scheduler":
		out, err = im.X.Output(ctx, "docker", "run", "--rm", "--network", "none", pinned, "version")
		if err == nil {
			var v, c string
			for _, l := range strings.Split(out, "\n") {
				f := strings.Fields(l)
				if len(f) == 2 && f[0] == "GitVersion:" {
					v = f[1]
				}
				if len(f) == 2 && f[0] == "GitCommit:" {
					c = f[1]
				}
			}
			return strings.TrimSpace(v + " (" + c + ")")
		}
	case "client", "driver":
		out, err = im.X.Output(ctx, "docker", "run", "--rm", "--network", "none", "--entrypoint", "dfget", pinned, "--version")
		if err == nil {
			return out
		}
	}
	return "unknown"
}

// BuildDerived builds a local image FROM base with dfbench copied to
// /opt/dfperf/dfbench, using create/cp/commit so the base layers stay
// byte-identical to the base digest and no registry access is needed
// (BuildKit insists on resolving FROM metadata remotely).
func (im *Images) BuildDerived(ctx context.Context, component, base, repo, binary string) (results.Image, error) {
	img := results.Image{Component: component, Ref: base, Platform: "linux/" + im.Arch, Source: "derived"}
	sum, err := fileSHA256(binary)
	if err != nil {
		return img, err
	}
	baseIns, err := im.inspect(ctx, base)
	if err != nil {
		return img, err
	}
	tag := fmt.Sprintf("%s:%s-%s", repo, strings.TrimPrefix(baseIns.ID, "sha256:")[:12], sum[:12])
	if ins, err := im.inspect(ctx, tag); err == nil && ins.Config.Labels["dfperf.dfbench.sha256"] == sum {
		img.Pinned, img.ImageID = tag, ins.ID
		return img, nil
	}
	stage := filepath.Join(im.WorkDir, "stage-"+sum[:12])
	if err := os.MkdirAll(filepath.Join(stage, "opt", "dfperf"), 0o755); err != nil {
		return img, err
	}
	if err := copyFile(binary, filepath.Join(stage, "opt", "dfperf", "dfbench"), 0o755); err != nil {
		return img, err
	}
	id, err := im.X.Output(ctx, "docker", "create", "--label", "dfperf.build=1", base)
	if err != nil {
		return img, err
	}
	defer im.X.Output(context.Background(), "docker", "rm", "-f", id)
	if _, err := im.X.Output(ctx, "docker", "cp", filepath.Join(stage, "opt"), id+":/"); err != nil {
		return img, err
	}
	if _, err := im.X.Output(ctx, "docker", "commit",
		"--change", `ENTRYPOINT ["/opt/dfperf/dfbench"]`, "--change", "CMD []",
		"--change", "LABEL dfperf.base="+base, "--change", "LABEL dfperf.base.id="+baseIns.ID,
		"--change", "LABEL dfperf.dfbench.sha256="+sum, id, tag); err != nil {
		return img, err
	}
	ins, err := im.inspect(ctx, tag)
	if err != nil {
		return img, err
	}
	img.Pinned, img.ImageID = tag, ins.ID
	return img, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
