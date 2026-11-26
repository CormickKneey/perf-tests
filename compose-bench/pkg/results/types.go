// Package results defines the on-disk JSON documents written under
// results/<run>/. The JSON shapes are described by schemas/*.schema.json and
// consumed by the dashboard; keep field names stable.
package results

import "time"

// SchemaVersion is bumped on incompatible changes of the result documents.
const SchemaVersion = "dfperf/v1"

// Cache states measured in every block.
const (
	StateCold      = "cold"
	StateSeedWarm  = "seed-warm"
	StatePeerWarm  = "peer-warm"
	StateLocalHit  = "local-hit"
	VariantBase    = "baseline"
	VariantCand    = "candidate"
	ModeSmoke      = "smoke"
	ModeCompare    = "compare"
	BlockOK        = "ok"
	BlockInvalid   = "invalid"
	BlockFailed    = "failed"
	BlockInfraFail = "infra-error"
)

// AllStates lists cache states in measurement order.
var AllStates = []string{StateCold, StateSeedWarm, StatePeerWarm, StateLocalHit}

// Traffic is a per-download delta of dfdaemon download traffic by type,
// taken from dragonfly_client_download_traffic{type=...}.
type Traffic struct {
	BackToSource int64 `json:"back_to_source"`
	RemotePeer   int64 `json:"remote_peer"`
	LocalPeer    int64 `json:"local_peer"`
}

// Sample is one measured dfget download (samples.jsonl line).
type Sample struct {
	Run     string `json:"run"`
	Variant string `json:"variant"`
	Block   int    `json:"block"`
	// Role is "measure" (used by compare), "prep" (cache priming) or "warmup".
	Role       string    `json:"role"`
	State      string    `json:"state"`
	File       string    `json:"file"`
	SizeBytes  int64     `json:"size_bytes"`
	Index      int       `json:"index"`
	Peer       string    `json:"peer"`
	Tag        string    `json:"tag"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
	ExitCode   int       `json:"exit_code"`
	DurationMs float64   `json:"duration_ms"`
	MiBps      float64   `json:"mib_per_s"`
	SHA256OK   bool      `json:"sha256_ok"`
	StartedAt  time.Time `json:"started_at"`
	// Traffic is the delta on the measured peer.
	Traffic Traffic `json:"traffic"`
	// SeedBackToSource is the seed-client back-to-source delta during the sample.
	SeedBackToSource int64 `json:"seed_back_to_source"`
	// SchedulerNormalBackToSource / SchedulerSuperBackToSource are deltas of
	// dragonfly_scheduler_traffic{type="BACK_TO_SOURCE"} by host_type.
	SchedulerNormalBackToSource int64 `json:"scheduler_normal_back_to_source"`
	SchedulerSuperBackToSource  int64 `json:"scheduler_super_back_to_source"`
	// OriginBytes maps peer name -> piece body bytes served by origin during
	// the sample; OriginProbes counts 1-byte `Range: bytes=0-0` probes.
	OriginBytes  map[string]int64 `json:"origin_bytes"`
	OriginProbes map[string]int   `json:"origin_probes"`
	MetricsError string           `json:"metrics_error,omitempty"`
}

// Check is a named validity assertion evaluated by the runner.
type Check struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Required checks invalidate the block (exit code 3) when they fail.
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

// Block is results/<run>/blocks/<id>/block.json.
type Block struct {
	Schema     string    `json:"schema"`
	Run        string    `json:"run"`
	ID         string    `json:"id"`
	Project    string    `json:"project"`
	Variant    string    `json:"variant"`
	Index      int       `json:"index"`
	Order      int       `json:"order"`
	Status     string    `json:"status"`
	Reasons    []string  `json:"reasons,omitempty"`
	Checks     []Check   `json:"checks"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Samples    int       `json:"samples"`
	Failures   int       `json:"failures"`
	// Timings of the compose lifecycle in seconds.
	UpSeconds      float64 `json:"up_seconds"`
	RunnerSeconds  float64 `json:"runner_seconds"`
	CleanupSeconds float64 `json:"cleanup_seconds"`
	RunnerExitCode int     `json:"runner_exit_code"`
}

// Image records how a component ref was resolved.
type Image struct {
	Component   string   `json:"component"`
	Ref         string   `json:"ref"`
	Pinned      string   `json:"pinned"`
	ImageID     string   `json:"image_id"`
	RepoDigests []string `json:"repo_digests"`
	// RegistryDigest is the (multi-arch) index digest from the registry when known.
	RegistryDigest string `json:"registry_digest,omitempty"`
	Platform       string `json:"platform"`
	Version        string `json:"version,omitempty"`
	Source         string `json:"source"`
}

// Variant describes baseline or candidate.
type Variant struct {
	Name      string   `json:"name"`
	Scheduler Image    `json:"scheduler"`
	Client    Image    `json:"client"`
	Driver    Image    `json:"driver"`
	Overlays  []string `json:"overlays"`
}

// Host captures the execution environment.
type Host struct {
	Hostname         string `json:"hostname"`
	OS               string `json:"os"`
	Kernel           string `json:"kernel"`
	Arch             string `json:"arch"`
	CPUs             int    `json:"cpus"`
	MemoryBytes      int64  `json:"memory_bytes"`
	DockerVersion    string `json:"docker_version"`
	ComposeVersion   string `json:"compose_version"`
	DockerOS         string `json:"docker_os"`
	DockerKernel     string `json:"docker_kernel"`
	DockerCPUs       int    `json:"docker_cpus"`
	DockerMemory     int64  `json:"docker_memory_bytes"`
	StorageDriver    string `json:"storage_driver"`
	Runtime          string `json:"runtime"`
	BaselineEligible bool   `json:"baseline_eligible"`
	Classification   string `json:"classification"`
	Note             string `json:"note"`
}

// Run is results/<run>/run.json.
type Run struct {
	Schema    string            `json:"schema"`
	ID        string            `json:"id"`
	Mode      string            `json:"mode"`
	Profile   string            `json:"profile"`
	Params    map[string]string `json:"params"`
	Baseline  Variant           `json:"baseline"`
	Candidate *Variant          `json:"candidate,omitempty"`
	// Support lists the tools base, tools and origin images.
	Support     []Image   `json:"support_images"`
	Host        Host      `json:"host"`
	NonBaseline bool      `json:"non_baseline"`
	BlockOrder  []string  `json:"block_order"`
	Fixtures    Fixtures  `json:"fixtures"`
	GitCommit   string    `json:"git_commit"`
	GitDirty    bool      `json:"git_dirty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exit_code"`
	Verdict     string    `json:"verdict"`
	Notes       []string  `json:"notes,omitempty"`
}

// FixtureFile is one generated file.
type FixtureFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Fixtures is the manifest written by the fixtures job.
type Fixtures struct {
	Schema    string        `json:"schema"`
	Volume    string        `json:"volume"`
	SpecHash  string        `json:"spec_hash"`
	Seed      string        `json:"seed"`
	Generator string        `json:"generator"`
	Files     []FixtureFile `json:"files"`
}
