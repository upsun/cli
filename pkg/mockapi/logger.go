package mockapi

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger logs requests to the test log, so that parallel tests' logs are not interleaved.
func RequestLogger(t testing.TB) func(http.Handler) http.Handler {
	return middleware.RequestLogger(&middleware.DefaultLogFormatter{Logger: testLogger{t}, NoColor: true})
}

type testLogger struct{ t testing.TB }

func (l testLogger) Print(v ...any) { l.t.Log(v...) }
