package mockapi

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// The API's OpenAPI spec, from https://meta.upsun.com/openapi-spec (refresh it with `make update-openapi-spec`).
//
//go:embed openapi.json
var openAPISpec []byte

// notInSpec lists requests that are not in the OpenAPI spec, so they are not validated.
// They are "METHOD /path" patterns for path.Match, where "*" matches one path segment.
var notInSpec = []string{
	"GET /billing/profiles/*",
	"PATCH /billing/profiles/*",
	"GET /organizations/*/setup/options",
	"GET /organizations/*/trial",
	"GET /organizations/*/usage",
	"GET /subscriptions/*",
	"DELETE /subscriptions/*",
	"PATCH /projects/*",
	"GET /projects/*/activities/*/log",
	"POST /projects/*/environments/*/backups",
	"PATCH /projects/*/environments/*/deployments/next",
	"GET /projects/*/environments/*/settings",
	"PATCH /projects/*/environments/*/settings",
	"POST /projects/*/integrations/*/validate",
	"GET /projects/*/integrations/*/activities",
	// The Auth API's SSH keys, and the endpoint that says which API owns them.
	"GET /ssh-key-source",
	"* /users/*/ssh-keys",
	"* /users/*/ssh-keys/*",
	// The CLI follows an entrypoint link to /observability/, with a trailing slash, and to logs/query.
	"GET /projects/*/environments/*/observability/",
	"GET /projects/*/environments/*/observability/logs/query",
	// Test routes.
	"POST /projects/*/integrations/*/check",
	"* /curl-test",
	"* /organizations/*/curl-test",
	"* /projects/*/curl-test",
	"* /projects/*/environments/*/curl-test",
}

func isInSpec(req *http.Request) bool {
	for _, p := range notInSpec {
		// Match the escaped path, as the router does, so that "*" also matches IDs with an encoded slash.
		if ok, _ := path.Match(p, req.Method+" "+req.URL.EscapedPath()); ok {
			return false
		}
	}
	return true
}

// The requests router validates requests, and the responses router validates responses, with the spec's required
// properties removed: mock responses only include the properties that tests need.
var (
	loadRequestsRouter  = sync.OnceValues(func() (routers.Router, error) { return loadOpenAPIRouter(false) })
	loadResponsesRouter = sync.OnceValues(func() (routers.Router, error) { return loadOpenAPIRouter(true) })
)

func loadOpenAPIRouter(ignoreRequired bool) (routers.Router, error) {
	doc, err := openapi3.NewLoader().LoadFromData(openAPISpec)
	if err != nil {
		return nil, err
	}
	// Match paths on any host, as the mock server listens on localhost.
	doc.Servers = nil
	for _, p := range doc.Paths.Map() {
		p.Servers = nil
	}
	if err := patchSpec(doc); err != nil {
		return nil, err
	}
	if ignoreRequired {
		seen := map[*openapi3.Schema]bool{}
		for _, s := range doc.Components.Schemas {
			removeRequired(s, seen)
		}
		for _, p := range doc.Paths.Map() {
			for _, op := range p.Operations() {
				for _, r := range op.Responses.Map() {
					if r.Value != nil {
						for _, c := range r.Value.Content {
							removeRequired(c.Schema, seen)
						}
					}
				}
			}
		}
	}
	return gorillamux.NewRouter(doc)
}

func removeRequired(ref *openapi3.SchemaRef, seen map[*openapi3.Schema]bool) {
	if ref == nil || ref.Value == nil || seen[ref.Value] {
		return
	}
	s := ref.Value
	seen[s] = true
	s.Required = nil
	for _, p := range s.Properties {
		removeRequired(p, seen)
	}
	removeRequired(s.Items, seen)
	removeRequired(s.AdditionalProperties.Schema, seen)
	removeRequired(s.Not, seen)
	for _, l := range []openapi3.SchemaRefs{s.AllOf, s.AnyOf, s.OneOf} {
		for _, r := range l {
			removeRequired(r, seen)
		}
	}
}

// integrationTypes maps the spec's integration schema names (without "Integration") to their types.
var integrationTypes = map[string]string{
	"Bitbucket":       "bitbucket",
	"BitbucketServer": "bitbucket_server",
	"Blackfire":       "blackfire",
	"Email":           "health.email",
	"Fastly":          "fastly",
	"GitLab":          "gitlab",
	"Github":          "github",
	"HealthWebHook":   "health.webhook",
	"HttpLog":         "httplog",
	"NewRelic":        "newrelic",
	"OtlpLog":         "otlplog",
	"PagerDuty":       "health.pagerduty",
	"Script":          "script",
	"Slack":           "health.slack",
	"Splunk":          "splunk",
	"Sumologic":       "sumologic",
	"Syslog":          "syslog",
	"WebHook":         "webhook",
}

// patchSpec fixes differences between the spec and the API, where the API client depends on them.
func patchSpec(doc *openapi3.T) error { //nolint:funlen // A list of patches.
	schemas := doc.Components.Schemas
	object := openapi3.NewObjectSchema().NewRef()
	str := openapi3.NewStringSchema().NewRef()
	for _, s := range schemas {
		v := s.Value
		if v.AdditionalProperties.Has != nil && !*v.AdditionalProperties.Has && v.Properties["_links"] == nil {
			// HAL links, on any resource.
			v.WithPropertyRef("_links", object)
		}
	}
	// The activities started by an operation, or the resource it created or updated (see the client's Result class).
	schemas["AcceptedResponse"].Value.WithPropertyRef("_embedded", object)
	// Shown by project:info, and used by the api.vendor_filter config.
	schemas["Project"].Value.WithPropertyRef("vendor", str)
	schemas["ProjectReference"].Value.WithPropertyRef("vendor", str)
	// Only the type tells integrations apart, but the spec doesn't restrict it.
	for _, name := range []string{"Integration", "IntegrationCreateCreateInput"} {
		for _, ref := range schemas[name].Value.OneOf {
			prefix := strings.TrimPrefix(ref.Ref, "#/components/schemas/")
			prefix, _, _ = strings.Cut(prefix, "Integration")
			t, ok := integrationTypes[prefix]
			if !ok {
				return fmt.Errorf("unknown integration schema: %s", ref.Ref)
			}
			typ := *ref.Value.Properties["type"].Value
			typ.Enum = []any{t}
			ref.Value.Properties["type"] = typ.NewRef()
		}
	}
	// An update can leave out properties, including the type (and secrets, which the client can't read).
	integrationPatch := schemas["IntegrationPatch"].Value
	for _, ref := range integrationPatch.OneOf {
		ref.Value.Required = nil
	}
	integrationPatch.AnyOf, integrationPatch.OneOf = integrationPatch.OneOf, nil
	// The API returns an app's stack as configured, which the CLI supports in several forms (see TestEnvironmentXdebug).
	for _, kind := range []string{"webapps", "workers"} {
		app := schemas["Deployment"].Value.Properties[kind].Value.AdditionalProperties.Schema.Value
		app.Properties["stack"] = openapi3.NewSchema().NewRef()
	}
	// Domains have an ssl property, which adds a certificate when it is written (used by domain:add and domain:update).
	for _, name := range []string{
		"ProdDomainStorage", "ReplacementDomainStorage",
		"ProdDomainStorageCreateInput", "ReplacementDomainStorageCreateInput",
		"ProdDomainStoragePatch", "ReplacementDomainStoragePatch",
	} {
		schemas[name].Value.WithPropertyRef("ssl", object)
	}
	// An update can match both variants, as it applies to an existing domain.
	domainPatch := schemas["DomainPatch"].Value
	domainPatch.AnyOf, domainPatch.OneOf = domainPatch.OneOf, nil
	// An environment update is a JSON merge patch, so null removes the basic auth credentials.
	httpAccess := schemas["EnvironmentPatch"].Value.Properties["http_access"].Value
	basicAuth := *httpAccess.Properties["basic_auth"].Value
	basicAuth.Nullable = true
	httpAccess.Properties["basic_auth"] = basicAuth.NewRef()
	// A replacement domain has a replacement_for, otherwise a domain matches both variants.
	r := schemas["ReplacementDomainStorageCreateInput"].Value
	r.Required = append(r.Required, "replacement_for")
	// The spec only has the tasks' sizing, but the API returns their whole config (see the client's Task class).
	schemas["Deployment"].Value.Properties["tasks"].Value.AdditionalProperties.Schema = object
	// The spec nests each service's settings and triggers one level too deep, and omits the service's "enabled".
	services := schemas["AutoscalerSettings"].Value.Properties["services"].Value
	services.AdditionalProperties.Schema = schemas["AutoscalerServiceSettings"]
	for name, p := range schemas["AutoscalerTriggers"].Value.Properties {
		if trigger := p.Value.AdditionalProperties.Schema; trigger != nil {
			schemas["AutoscalerTriggers"].Value.Properties[name] = trigger
		}
	}
	// Each route type has its own schema, and an original_url (shown by route:list).
	routeTypes := map[string]string{"ProxyRoute": "proxy", "RedirectRoute": "redirect", "UpstreamRoute": "upstream"}
	for name, t := range routeTypes {
		route := schemas[name].Value
		typ := *route.Properties["type"].Value
		typ.Enum = []any{t}
		route.Properties["type"] = typ.NewRef()
		route.WithPropertyRef("original_url", str)
	}
	return nil
}

var validationOptions = &openapi3filter.Options{
	MultiError: true,
	// Check that the status is in the spec, otherwise the response isn't validated.
	IncludeResponseStatus: true,
	// Authentication is checked by the mock itself.
	AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	// Don't modify the request.
	SkipSettingDefaults: true,
	// Leave out the schema and value from errors, as they can be large.
	SchemaValidationOptions: []openapi3.SchemaValidationOption{openapi3.DisableSchemaErrorDetails()},
}

// validateOpenAPI checks requests and responses against the OpenAPI spec, and reports failures through the test.
func validateOpenAPI(t testing.TB) func(http.Handler) http.Handler {
	reqRouter, err := loadRequestsRouter()
	if err != nil {
		t.Fatalf("failed to load OpenAPI spec: %v", err)
	}
	respRouter, err := loadResponsesRouter()
	if err != nil {
		t.Fatalf("failed to load OpenAPI spec: %v", err)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			reqBody, err := io.ReadAll(req.Body)
			if err != nil {
				t.Errorf("failed to read request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			req.Body = io.NopCloser(bytes.NewReader(reqBody))

			// The API returns JSON, unless a handler says otherwise.
			w.Header().Set("Content-Type", "application/json")
			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, req)

			// Only successful exchanges are checked: the spec describes few errors, and some only by "default" responses
			// that are meant for success.
			if rec.status < 300 && isInSpec(req) {
				validateExchange(t, reqRouter, respRouter, req, reqBody, rec)
			}

			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body.Bytes())
		})
	}
}

func validateExchange(
	t testing.TB, reqRouter, respRouter routers.Router, req *http.Request, reqBody []byte, rec *responseRecorder,
) {
	input, err := requestValidationInput(reqRouter, req, reqBody)
	if err != nil {
		t.Errorf("OpenAPI: %s %s: %v", req.Method, req.URL.Path, err)
		return
	}
	if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
		t.Errorf("OpenAPI: invalid request: %s %s: %v", req.Method, req.URL, err)
	}
	input, err = requestValidationInput(respRouter, req, reqBody)
	if err != nil {
		t.Errorf("OpenAPI: %s %s: %v", req.Method, req.URL.Path, err)
		return
	}
	respInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 rec.status,
		Header:                 rec.Header(),
		Options:                validationOptions,
	}
	respInput.SetBodyBytes(rec.body.Bytes())
	if err := openapi3filter.ValidateResponse(context.Background(), respInput); err != nil {
		t.Errorf("OpenAPI: invalid response: %s %s (%d): %v", req.Method, req.URL, rec.status, err)
	}
}

func requestValidationInput(
	router routers.Router, req *http.Request, body []byte,
) (*openapi3filter.RequestValidationInput, error) {
	vReq := req.Clone(context.Background())
	vReq.Body = io.NopCloser(bytes.NewReader(body))
	route, pathParams, err := router.FindRoute(vReq)
	if err != nil {
		return nil, err
	}
	return &openapi3filter.RequestValidationInput{
		Request:    vReq,
		PathParams: pathParams,
		Route:      route,
		Options:    validationOptions,
	}, nil
}

// responseRecorder buffers a response, so that it can be validated before it is sent.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (r *responseRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = status, true
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.body.Write(b)
}
