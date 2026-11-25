package bench

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dragonflyoss/perf-tests/compose-bench/pkg/results"
)

// FixtureGenerator identifies the byte stream algorithm. Changing the
// algorithm must change this string so the spec hash (and volume) changes.
const FixtureGenerator = "chacha8-sha256seed-v1"

// ParseSize parses sizes such as 512KiB, 8MiB, 1GiB or plain bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, err := strconv.ParseInt(strings.TrimSuffix(s, u.suffix), 10, 64)
			if err != nil || n <= 0 {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return n * u.mult, nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n, nil
}

// FormatSize renders bytes in the shortest exact binary unit.
func FormatSize(n int64) string {
	switch {
	case n%(1<<30) == 0:
		return fmt.Sprintf("%dGiB", n>>30)
	case n%(1<<20) == 0:
		return fmt.Sprintf("%dMiB", n>>20)
	case n%(1<<10) == 0:
		return fmt.Sprintf("%dKiB", n>>10)
	}
	return fmt.Sprintf("%dB", n)
}

// ParseSizes parses a comma separated size list, deduplicated and sorted.
func ParseSizes(list string) ([]int64, error) {
	seen := map[int64]bool{}
	var out []int64
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		n, err := ParseSize(part)
		if err != nil {
			return nil, err
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no fixture sizes")
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// FixtureSpecHash is the content address of a fixture set. It is used for
// the external volume name dfperf-fixtures-<hash>.
func FixtureSpecHash(seed string, sizes []int64) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s", FixtureGenerator, seed)
	for _, s := range sizes {
		fmt.Fprintf(h, "|%d", s)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// FixtureVolumeName returns the external volume name for a fixture set.
func FixtureVolumeName(seed string, sizes []int64) string {
	return "dfperf-fixtures-" + FixtureSpecHash(seed, sizes)
}

// FixtureName is the file name for a size.
func FixtureName(size int64) string {
	return "f-" + strings.ToLower(FormatSize(size)) + ".bin"
}

// fixtureStream returns the deterministic byte stream of one file.
func fixtureStream(seed string, size int64) io.Reader {
	key := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", FixtureGenerator, seed, size)))
	return io.LimitReader(rand.NewChaCha8(key), size)
}

// GenerateFixtures writes files/<name> and manifest.json into dir. It is
// idempotent: when a manifest with the same spec hash exists and every file
// has the expected size, nothing is rewritten.
func GenerateFixtures(dir, seed string, sizes []int64, log io.Writer) (*results.Fixtures, error) {
	spec := FixtureSpecHash(seed, sizes)
	manifestPath := filepath.Join(dir, "manifest.json")
	if m, err := ReadManifest(manifestPath); err == nil && m.SpecHash == spec && filesPresent(dir, m) {
		fmt.Fprintf(log, "fixtures: manifest %s up to date (%d files)\n", spec, len(m.Files))
		return m, nil
	}
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		return nil, err
	}
	m := &results.Fixtures{
		Schema:    results.SchemaVersion,
		Volume:    "dfperf-fixtures-" + spec,
		SpecHash:  spec,
		Seed:      seed,
		Generator: FixtureGenerator,
	}
	for _, size := range sizes {
		name := FixtureName(size)
		rel := filepath.Join("files", name)
		sum, err := writeFixture(filepath.Join(dir, rel), seed, size)
		if err != nil {
			return nil, err
		}
		m.Files = append(m.Files, results.FixtureFile{Name: name, Path: "/files/" + name, Size: size, SHA256: sum})
		fmt.Fprintf(log, "fixtures: %s %d bytes sha256=%s\n", name, size, sum)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	tmp := manifestPath + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, manifestPath); err != nil {
		return nil, err
	}
	// Plain sha256sum-compatible list for humans and shell checks.
	var b strings.Builder
	for _, f := range m.Files {
		fmt.Fprintf(&b, "%s  files/%s\n", f.SHA256, f.Name)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return m, nil
}

func writeFixture(path, seed string, size int64) (string, error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	w := bufio.NewWriterSize(io.MultiWriter(f, h), 1<<20)
	if _, err := io.Copy(w, fixtureStream(seed, size)); err != nil {
		f.Close()
		return "", err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func filesPresent(dir string, m *results.Fixtures) bool {
	for _, f := range m.Files {
		st, err := os.Stat(filepath.Join(dir, "files", f.Name))
		if err != nil || st.Size() != f.Size {
			return false
		}
	}
	return len(m.Files) > 0
}

// ReadManifest loads a fixtures manifest.
func ReadManifest(path string) (*results.Fixtures, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m results.Fixtures
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ExpectedSHA256 computes the digest of a fixture without writing it.
func ExpectedSHA256(seed string, size int64) string {
	h := sha256.New()
	_, _ = io.Copy(h, fixtureStream(seed, size))
	return hex.EncodeToString(h.Sum(nil))
}
