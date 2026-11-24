//go:build ignore

// Command gen fetches the container image registry and the composable image
// channels from meta.upsun.com, and writes registry.json in the format consumed
// by the registry package.
//
// Run it with: go run gen.go (or `go generate ./pkg/registry`).
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/upsun/cli/pkg/registry"
)

const (
	imagesURL     = "https://meta.upsun.com/images"
	composableURL = "https://meta.upsun.com/composable"
)

// metaImage is the shape of an image entry served by meta.upsun.com/images, and
// of the composable image served by meta.upsun.com/composable.
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
	var images map[string]metaImage
	if err := fetch(client, imagesURL, &images); err != nil {
		return err
	}
	var composable metaImage
	if err := fetch(client, composableURL, &composable); err != nil {
		return err
	}
	if len(composable.Versions) == 0 {
		return fmt.Errorf("no versions found at %s", composableURL)
	}
	composable.Name = "Composable image"
	images["composable"] = composable

	reg := make(registry.Registry, len(images))
	for typeName, img := range images {
		var supported, legacy, retired []string
		for version, info := range img.Versions {
			switch info.Upsun.Status {
			case "supported":
				supported = append(supported, version)
			case "deprecated":
				// Deprecated versions still deploy, so treat them as legacy (allowed).
				legacy = append(legacy, version)
			case "retired":
				// Retired versions are allowed with a warning.
				retired = append(retired, version)
			}
			// "decommissioned" versions are omitted, so they fail linting.
		}
		sortVersionsDescending(supported)
		sortVersionsDescending(legacy)
		sortVersionsDescending(retired)
		reg[typeName] = registry.Image{
			Name:      img.Name,
			Type:      typeName,
			IsRuntime: !img.Service,
			Versions:  registry.VersionInfo{Supported: supported, Legacy: legacy, Retired: retired},
		}
	}

	out, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("registry.json", append(out, '\n'), 0o644)
}

func fetch(client *http.Client, url string, v any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s from %s", resp.Status, url)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// sortVersionsDescending sorts version strings newest-first (e.g. 8.3 before 8.1).
func sortVersionsDescending(versions []string) {
	slices.SortFunc(versions, func(a, b string) int {
		return registry.CompareVersions(b, a)
	})
}
