package f1net

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// parseSources parses the dashboard source list. Each non-empty line has the
// form "Name | URL"; anything after the first "|" is the URL.
func parseSources(r io.Reader) ([]Source, error) {
	var out []Source
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		name, url, ok := strings.Cut(line, "|")
		if !ok {
			return nil, fmt.Errorf("invalid source line %q", line)
		}
		out = append(out, Source{
			Name: strings.TrimSpace(name),
			URL:  strings.TrimSpace(url),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
