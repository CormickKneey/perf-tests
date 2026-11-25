package bench

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PromSample is one series value from the Prometheus text format.
type PromSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// ParseProm parses the Prometheus text exposition format (no exemplars,
// no timestamps needed). Unknown/malformed lines are skipped.
func ParseProm(r io.Reader) []PromSample {
	var out []PromSample
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var name, rest string
		labels := map[string]string{}
		if i := strings.IndexByte(line, '{'); i >= 0 {
			j := strings.LastIndexByte(line, '}')
			if j < i {
				continue
			}
			name = line[:i]
			parseLabels(line[i+1:j], labels)
			rest = strings.TrimSpace(line[j+1:])
		} else {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			name, rest = f[0], strings.Join(f[1:], " ")
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		out = append(out, PromSample{Name: name, Labels: labels, Value: v})
	}
	return out
}

func parseLabels(s string, into map[string]string) {
	for len(s) > 0 {
		eq := strings.IndexByte(s, '=')
		if eq < 0 || eq+1 >= len(s) || s[eq+1] != '"' {
			return
		}
		key := strings.TrimSpace(strings.TrimLeft(s[:eq], ", "))
		var b strings.Builder
		i := eq + 2
		for ; i < len(s); i++ {
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				default:
					b.WriteByte(s[i])
				}
				continue
			}
			if c == '"' {
				break
			}
			b.WriteByte(c)
		}
		into[key] = b.String()
		if i+1 >= len(s) {
			return
		}
		s = s[i+1:]
	}
}

// Sum adds all series of a metric whose labels match every given pair.
func Sum(samples []PromSample, name string, match map[string]string) float64 {
	var total float64
	for _, s := range samples {
		if s.Name != name {
			continue
		}
		ok := true
		for k, v := range match {
			if !strings.EqualFold(s.Labels[k], v) {
				ok = false
				break
			}
		}
		if ok {
			total += s.Value
		}
	}
	return total
}

// Scrape fetches a /metrics endpoint and returns raw text.
func Scrape(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("scrape %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return string(b), err
}
