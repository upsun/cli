// Package routeloops detects redirect loops in a project's routes.
//
// A cycle is any chain of `type: redirect` routes whose `to:` values close a
// loop, including self-loops. Path-based `redirects.paths:` blocks are out of
// scope for this package.
package routeloops

import (
	"sort"
	"strings"
	"unicode"
)

const (
	TypeUpstream = "upstream"
	TypeRedirect = "redirect"
)

// Route is a single entry from a project's routes configuration.
type Route struct {
	URL  string
	Type string
	To   string
}

// Cycle is a sequence of route URLs that redirect back to the first entry.
// URLs[0] and URLs[len-1] are the same value.
type Cycle struct {
	URLs []string
}

// Warning flags a redirect route that could not be analyzed.
type Warning struct {
	URL    string
	Reason string
}

// Result is the outcome of Detect.
type Result struct {
	Cycles   []Cycle
	Warnings []Warning
}

// Detect finds redirect cycles among the given routes.
func Detect(routes []Route) Result {
	edges := make(map[string]string, len(routes))
	known := make(map[string]struct{}, len(routes))
	var warnings []Warning

	for _, r := range routes {
		url := normalize(r.URL)
		if url == "" {
			continue
		}
		known[url] = struct{}{}
		if r.Type != TypeRedirect {
			continue
		}
		to := normalize(r.To)
		if to == "" {
			warnings = append(warnings, Warning{
				URL:    r.URL,
				Reason: "redirect route has no `to:` (uses `redirects.paths` only, or is malformed)",
			})
			continue
		}
		edges[url] = to
	}

	for from, to := range edges {
		if _, ok := known[to]; !ok {
			warnings = append(warnings, Warning{
				URL:    from,
				Reason: "redirect target is not a known route: " + to,
			})
		}
	}

	visited := make(map[string]struct{}, len(edges))
	seenCycles := make(map[string]struct{})
	var cycles []Cycle

	for start := range edges {
		if _, ok := visited[start]; ok {
			continue
		}
		walk := []string{start}
		walkIndex := map[string]int{start: 0}
		current := start
		for {
			next, ok := edges[current]
			if !ok {
				break
			}
			if idx, seen := walkIndex[next]; seen {
				cycleNodes := append([]string(nil), walk[idx:]...)
				cycle := canonicalizeCycle(cycleNodes)
				key := strings.Join(cycle, "\x00")
				if _, dup := seenCycles[key]; !dup {
					seenCycles[key] = struct{}{}
					closed := append(cycle, cycle[0])
					cycles = append(cycles, Cycle{URLs: closed})
				}
				break
			}
			walkIndex[next] = len(walk)
			walk = append(walk, next)
			current = next
		}
		for _, node := range walk {
			visited[node] = struct{}{}
		}
	}

	sort.Slice(cycles, func(i, j int) bool {
		return cycles[i].URLs[0] < cycles[j].URLs[0]
	})
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].URL != warnings[j].URL {
			return warnings[i].URL < warnings[j].URL
		}
		return warnings[i].Reason < warnings[j].Reason
	})

	return Result{Cycles: cycles, Warnings: warnings}
}

// canonicalizeCycle rotates the cycle so it starts at its lexicographically
// smallest URL. The returned slice does NOT repeat the first element at the end.
func canonicalizeCycle(nodes []string) []string {
	if len(nodes) <= 1 {
		return nodes
	}
	minIdx := 0
	for i := 1; i < len(nodes); i++ {
		if nodes[i] < nodes[minIdx] {
			minIdx = i
		}
	}
	rotated := make([]string, 0, len(nodes))
	rotated = append(rotated, nodes[minIdx:]...)
	rotated = append(rotated, nodes[:minIdx]...)
	return rotated
}

// normalize prepares a URL string for edge lookup. It strips whitespace and
// smart quotes, lowercases the scheme+host, and trims trailing slashes.
// Placeholders like {default} are preserved as-is.
func normalize(s string) string {
	s = strings.TrimFunc(s, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case '“', '”', '‘', '’', '"', '\'':
			return true
		}
		return false
	})
	if s == "" {
		return s
	}
	schemeEnd := strings.Index(s, "://")
	if schemeEnd < 0 {
		return strings.TrimRight(s, "/")
	}
	scheme := strings.ToLower(s[:schemeEnd])
	rest := s[schemeEnd+3:]
	pathStart := strings.IndexAny(rest, "/?#")
	if pathStart < 0 {
		return scheme + "://" + strings.ToLower(rest)
	}
	host := strings.ToLower(rest[:pathStart])
	path := rest[pathStart:]
	if path == "/" {
		path = ""
	} else {
		path = strings.TrimRight(path, "/")
	}
	return scheme + "://" + host + path
}
