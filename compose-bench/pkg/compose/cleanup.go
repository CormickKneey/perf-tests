package compose

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Clean removes leftover dfperf block projects (containers, named volumes,
// networks) and optionally the external fixtures volumes. Every name is
// checked against the SPEC patterns first; nothing else is ever touched and
// `docker system prune` is never used.
func Clean(ctx context.Context, x *Exec, runID string, fixtures, dryRun bool) error {
	projects := map[string]bool{}
	for _, kind := range [][]string{
		{"ps", "-a", "--format", `{{.Label "com.docker.compose.project"}}`},
		{"volume", "ls", "--format", `{{.Label "com.docker.compose.project"}}`},
		{"network", "ls", "--format", `{{.Label "com.docker.compose.project"}}`},
	} {
		out, err := x.Output(ctx, "docker", kind...)
		if err != nil {
			return err
		}
		for _, p := range strings.Fields(out) {
			if strings.HasPrefix(p, "dfperf-") {
				projects[p] = true
			}
		}
	}
	names := make([]string, 0, len(projects))
	for p := range projects {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		if err := ValidateProject(p); err != nil {
			fmt.Fprintf(x.Log, "skip %s: %v\n", p, err)
			continue
		}
		if runID != "" && !strings.HasPrefix(p, "dfperf-"+runID+"-") {
			continue
		}
		fmt.Fprintf(x.Log, "remove project %s\n", p)
		if dryRun {
			continue
		}
		if err := removeProject(ctx, x, p); err != nil {
			return err
		}
	}
	if fixtures {
		out, err := x.Output(ctx, "docker", "volume", "ls", "-q", "--filter", "name=dfperf-fixtures-")
		if err != nil {
			return err
		}
		for _, v := range strings.Fields(out) {
			if err := ValidateFixturesVolume(v); err != nil {
				fmt.Fprintf(x.Log, "skip %s: %v\n", v, err)
				continue
			}
			fmt.Fprintf(x.Log, "remove fixtures volume %s\n", v)
			if dryRun {
				continue
			}
			if _, err := x.Output(ctx, "docker", "volume", "rm", v); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeProject deletes resources by compose project label, without needing
// the project's compose file or environment.
func removeProject(ctx context.Context, x *Exec, project string) error {
	if err := ValidateProject(project); err != nil {
		return err
	}
	filter := "label=com.docker.compose.project=" + project
	if ids, _ := x.Output(ctx, "docker", "ps", "-aq", "--filter", filter); ids != "" {
		if _, err := x.Output(ctx, "docker", append([]string{"rm", "-f", "-v"}, strings.Fields(ids)...)...); err != nil {
			return err
		}
	}
	if vols, _ := x.Output(ctx, "docker", "volume", "ls", "-q", "--filter", filter); vols != "" {
		for _, v := range strings.Fields(vols) {
			if !strings.HasPrefix(v, project+"_") {
				return fmt.Errorf("volume %s of %s has unexpected name", v, project)
			}
			if _, err := x.Output(ctx, "docker", "volume", "rm", v); err != nil {
				return err
			}
		}
	}
	if nets, _ := x.Output(ctx, "docker", "network", "ls", "-q", "--filter", filter); nets != "" {
		if _, err := x.Output(ctx, "docker", append([]string{"network", "rm"}, strings.Fields(nets)...)...); err != nil {
			return err
		}
	}
	return VerifyGone(ctx, x, project)
}
