//go:build ignore

// Command gen fetches the Upsun API's OpenAPI spec from meta.upsun.com and
// writes it, indented, to openapi.json.
//
// Run it with: go run gen.go (or `make update-openapi-spec`).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const specURL = "https://meta.upsun.com/openapi-spec"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(specURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s from %s", resp.Status, specURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	return os.WriteFile("openapi.json", out.Bytes(), 0o644)
}
