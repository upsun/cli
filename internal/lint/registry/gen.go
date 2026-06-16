//go:build ignore

// Command gen fetches the container image registry from meta.upsun.com and
// writes registry.json in the format consumed by the registry package.
//
// Run it with: go run gen.go (or `go generate ./internal/lint/registry`).
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/upsun/cli/internal/lint/registry"
)

const imagesURL = "https://meta.upsun.com/images"

// metaImage is the shape of an image entry served by meta.upsun.com/images.
type metaImage struct {
	Name     string `json:"name"`
	Service  bool   `json:"service"`
	Versions map[string]struct {
		Upsun struct {
			Status string `json:"status"`
		} `json:"upsun"`
	} `json:"versions"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(imagesURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s from %s", resp.Status, imagesURL)
	}

	var images map[string]metaImage
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return err
	}

	reg := make(registry.Registry, len(images))
	for typeName, img := range images {
		var supported, legacy []string
		for version, info := range img.Versions {
			switch info.Upsun.Status {
			case "supported":
				supported = append(supported, version)
			case "deprecated":
				// Deprecated versions still deploy, so treat them as legacy (allowed).
				legacy = append(legacy, version)
			}
			// "retired" and "decommissioned" versions are omitted, so they fail linting.
		}
		sortVersionsDescending(supported)
		sortVersionsDescending(legacy)
		reg[typeName] = registry.Image{
			Name:      img.Name,
			Type:      typeName,
			IsRuntime: !img.Service,
			Versions:  registry.VersionInfo{Supported: supported, Legacy: legacy},
		}
	}

	out, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("registry.json", append(out, '\n'), 0o644)
}

// sortVersionsDescending sorts version strings newest-first (e.g. 8.3 before 8.1).
func sortVersionsDescending(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})
}

// compareVersions compares dotted numeric version strings, falling back to
// string comparison for non-numeric parts.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea == nil && eb == nil {
			if na != nb {
				return na - nb
			}
			continue
		}
		if c := strings.Compare(pa[i], pb[i]); c != 0 {
			return c
		}
	}
	return len(pa) - len(pb)
}
