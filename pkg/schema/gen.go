//go:build ignore

// Command gen fetches the Upsun configuration schema from meta.upsun.com and
// writes it, indented, to upsun-config-schema.json.
//
// Run it with: go run gen.go (or `go generate ./pkg/schema`).
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

const schemaURL = "https://meta.upsun.com/schema/upsun"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(schemaURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s from %s", resp.Status, schemaURL)
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
	return os.WriteFile("upsun-config-schema.json", out.Bytes(), 0o644)
}
