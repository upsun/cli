package mockapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

var (
	openAPIDoc *openapi3.T
	router     routers.Router
	loadOnce   sync.Once
	loadErr    error
)

// findModuleRoot walks up the directory tree to find go.mod
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

// loadOpenAPISpec loads and validates the OpenAPI spec (once per test run)
func loadOpenAPISpec() error {
	loadOnce.Do(func() {
		loader := openapi3.NewLoader()
		loader.IsExternalRefsAllowed = true

		// Find module root and construct path to spec
		moduleRoot, err := findModuleRoot()
		if err != nil {
			loadErr = fmt.Errorf("failed to find module root: %w", err)
			return
		}

		specPath := filepath.Join(moduleRoot, "pkg/mockapi/testdata/upsun-openapi.json")

		openAPIDoc, loadErr = loader.LoadFromFile(specPath)
		if loadErr != nil {
			return
		}

		loadErr = openAPIDoc.Validate(loader.Context)
		if loadErr != nil {
			return
		}

		// Remove servers section for mock testing
		// The spec defines servers as "https://api.upsun.com" but our mock runs on localhost
		openAPIDoc.Servers = nil

		router, loadErr = legacy.NewRouter(openAPIDoc)
	})
	return loadErr
}

// OpenAPIValidationMiddleware validates mock API responses against OpenAPI spec
// Enable by setting VALIDATE_OPENAPI=1 environment variable
func OpenAPIValidationMiddleware(t testing.TB) func(http.Handler) http.Handler {
	// Only validate if explicitly enabled
	if os.Getenv("VALIDATE_OPENAPI") == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	if err := loadOpenAPISpec(); err != nil {
		t.Logf("Warning: OpenAPI validation disabled - failed to load spec: %v", err)
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Find route in OpenAPI spec
			route, pathParams, err := router.FindRoute(r)
			if err != nil {
				t.Logf("Warning: Route not in OpenAPI spec: %s %s - %v", r.Method, r.URL.Path, err)
				next.ServeHTTP(w, r)
				return
			}

			// Capture response
			rec := &responseCapture{
				ResponseWriter: w,
				body:           &bytes.Buffer{},
				statusCode:     http.StatusOK, // Default status
			}
			next.ServeHTTP(rec, r)

			// Validate response against OpenAPI schema
			responseValidationInput := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: &openapi3filter.RequestValidationInput{
					Request:    r,
					PathParams: pathParams,
					Route:      route,
				},
				Status: rec.statusCode,
				Header: rec.Header(),
				Body:   io.NopCloser(bytes.NewReader(rec.body.Bytes())),
				Options: &openapi3filter.Options{
					IncludeResponseStatus: true,
				},
			}

			if err := openapi3filter.ValidateResponse(context.Background(), responseValidationInput); err != nil {
				t.Errorf("OpenAPI validation failed for %s %s (status %d):\n%v\nResponse body:\n%s",
					r.Method, r.URL.Path, rec.statusCode, err, rec.body.String())
			}
		})
	}
}

// responseCapture captures the response for validation
type responseCapture struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (r *responseCapture) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseCapture) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

