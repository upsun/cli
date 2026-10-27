package mockapi

// TODO unify these models with the 'api' package, and/or use OpenAPI or similar to generate them

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// refSig is the signature that the API adds to reference links.
const refSig = "&sig=mock-signature"

type HALLink struct {
	HREF string `json:"href"`
}

type HalLinks map[string]HALLink

// MakeHALLinks helps make a list of HAL links out of arguments in the format name=path.
func MakeHALLinks(nameAndPath ...string) HalLinks {
	links := make(HalLinks, len(nameAndPath))
	for _, p := range nameAndPath {
		parts := strings.SplitN(p, "=", 2)
		links[parts[0]] = HALLink{parts[1]}
	}
	return links
}

type Subscription struct {
	ID            string   `json:"id"`
	Links         HalLinks `json:"_links,omitempty"`
	ProjectID     string   `json:"project_id"`
	ProjectRegion string   `json:"project_region"`
	ProjectTitle  string   `json:"project_title"`
	Status        string   `json:"status"`
	ProjectUI     string   `json:"project_ui"`
}

type CanCreateRequiredAction struct {
	Action string `json:"action"`
	Type   string `json:"type"`
}

type CanCreateResponse struct {
	CanCreate bool   `json:"can_create"`
	Message   string `json:"message"`

	RequiredAction *CanCreateRequiredAction `json:"required_action"`
}

type ProjectRepository struct {
	URL string `json:"url"`
}

type Project struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Region        string            `json:"region"`
	Organization  string            `json:"organization"`
	Owner         string            `json:"owner,omitempty"`
	Vendor        string            `json:"vendor"`
	Repository    ProjectRepository `json:"repository"`
	DefaultBranch string            `json:"default_branch"`
	Links         HalLinks          `json:"_links,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`

	Subscription ProjectSubscriptionInfo `json:"subscription,omitempty"`

	SubscriptionID string `json:"-"`

	// Settings are served at the project's /settings path, if set.
	Settings map[string]any `json:"-"`
}

type ProjectSubscriptionInfo struct {
	LicenseURI string `json:"license_uri,omitempty"`

	Plan          string `json:"plan,omitempty"`
	Environments  int    `json:"environments,omitempty"`
	Storage       int    `json:"storage,omitempty"`
	IncludedUsers int    `json:"included_users,omitempty"`
	UserLicenses  int    `json:"user_licenses,omitempty"`

	ManagementURI string `json:"subscription_management_uri,omitempty"`

	Restricted bool `json:"restricted,omitempty"`
	Suspended  bool `json:"suspended,omitempty"`
}

func (p *Project) AsRef() *ProjectRef {
	return &ProjectRef{
		ID:             p.ID,
		Region:         p.Region,
		Title:          p.Title,
		Status:         "active",
		OrganizationID: p.Organization,
		SubscriptionID: p.SubscriptionID,
		Vendor:         p.Vendor,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

type Environment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	MachineName string    `json:"machine_name"`
	Title       string    `json:"title"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	Parent      any       `json:"parent"`
	Project     string    `json:"project"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Links       HalLinks  `json:"_links,omitempty"`

	HTTPAccess       map[string]any `json:"http_access,omitempty"`
	HeadCommit       string         `json:"head_commit,omitempty"`
	MergeInfo        map[string]any `json:"merge_info,omitempty"`
	CanRollingDeploy bool           `json:"can_rolling_deploy,omitempty"`

	currentDeployment   *Deployment
	nextDeployment      map[string]any
	deploymentPatches   []map[string]any
	autoscalingSettings map[string]any
	settings            map[string]any
	actions             []EnvironmentAction
}

func (e *Environment) SetCurrentDeployment(d *Deployment) {
	e.currentDeployment = d
}

// SetNextDeployment sets the next deployment, which is served as raw data so
// that tests can include fields the Deployment type lacks. PATCH requests to
// it are recorded, and returned by Handler.DeploymentPatches.
func (e *Environment) SetNextDeployment(d map[string]any) {
	e.nextDeployment = d
}

// SetAutoscalingSettings sets the autoscaling settings, and the environment
// links needed to read and manage them.
func (e *Environment) SetAutoscalingSettings(s map[string]any) {
	e.autoscalingSettings = s
	if e.Links == nil {
		e.Links = make(HalLinks)
	}
	path := "/projects/" + url.PathEscape(e.Project) + "/environments/" + url.PathEscape(e.ID) + "/autoscaling/settings"
	e.Links["#autoscaling"] = HALLink{HREF: path}
	e.Links["#manage-autoscaling"] = HALLink{HREF: path}
}

func (e *Environment) SetSetting(key string, val any) {
	if e.settings == nil {
		e.settings = make(map[string]any)
	}
	e.settings[key] = val
}

type Mount struct {
	Source     string `json:"source"`
	SourcePath string `json:"source_path"`
}

type App struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size string `json:"size,omitempty"`
	Disk int    `json:"disk"`

	Mounts map[string]Mount `json:"mounts"`

	Web     map[string]any `json:"web,omitempty"`
	Runtime map[string]any `json:"runtime,omitempty"`
	Stack   any            `json:"stack,omitempty"`
}

// withMounts returns a copy of the app with an empty mounts object instead of null.
func (a App) withMounts() App { //nolint:gocritic // A copy is returned.
	if a.Mounts == nil {
		a.Mounts = map[string]Mount{}
	}
	return a
}

type Commands struct {
	Start string `json:"start"`
}

type WorkerInfo struct {
	Commands Commands `json:"commands"`
}

type Worker struct {
	App
	Worker WorkerInfo `json:"worker"`
}

// Service is a service in a deployment.
type Service struct {
	Type string `json:"type"`
	Size string `json:"size,omitempty"`
	Disk int    `json:"disk,omitempty"`
}

type Deployment struct {
	WebApps  map[string]App     `json:"webapps"`
	Services map[string]Service `json:"services"`
	Workers  map[string]Worker  `json:"workers"`
	Tasks    map[string]App     `json:"tasks,omitempty"`

	Routes map[string]any `json:"routes"`

	Links HalLinks `json:"_links,omitempty"`
}

// MarshalJSON outputs empty objects instead of null, as the API does.
func (d Deployment) MarshalJSON() ([]byte, error) {
	type deployment Deployment
	c := deployment(d)
	c.WebApps = make(map[string]App, len(d.WebApps))
	for name, a := range d.WebApps {
		c.WebApps[name] = a.withMounts()
	}
	c.Workers = make(map[string]Worker, len(d.Workers))
	for name, w := range d.Workers {
		w.App = w.withMounts()
		c.Workers[name] = w
	}
	if c.Services == nil {
		c.Services = map[string]Service{}
	}
	if c.Routes == nil {
		c.Routes = map[string]any{}
	}
	return json.Marshal(c)
}

type Org struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Owner        string   `json:"owner_id"`
	Capabilities []string `json:"capabilities"`
	Links        HalLinks `json:"_links,omitempty"`

	BillingLegacy    *bool  `json:"billing_legacy,omitempty"`
	BillingProfileID string `json:"billing_profile_id,omitempty"`
}

// MarshalJSON outputs an empty list of capabilities instead of null.
func (o Org) MarshalJSON() ([]byte, error) { //nolint:gocritic // So that it applies to values too.
	type org Org
	c := org(o)
	if c.Capabilities == nil {
		c.Capabilities = []string{}
	}
	return json.Marshal(c)
}

func (o *Org) AsRef() *OrgRef {
	return &OrgRef{
		ID:    o.ID,
		Type:  o.Type,
		Name:  o.Name,
		Label: o.Label,
		Owner: o.Owner,
	}
}

type OrgRef struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Owner string `json:"owner_id"`
}

type ProjectUserGrant struct {
	ProjectID      string    `json:"project_id"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Permissions    []string  `json:"permissions"`
	GrantedAt      time.Time `json:"granted_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type UserGrant struct {
	ResourceID     string    `json:"resource_id"`
	ResourceType   string    `json:"resource_type"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Permissions    []string  `json:"permissions"`
	GrantedAt      time.Time `json:"granted_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type UserRef struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type ProjectRef struct {
	ID             string    `json:"id"`
	Region         string    `json:"region"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	OrganizationID string    `json:"organization_id"`
	SubscriptionID string    `json:"subscription_id"`
	Vendor         string    `json:"vendor"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type User struct {
	ID string `json:"id"`

	Deactivated bool `json:"deactivated"`

	Namespace string `json:"namespace"`

	Username      string `json:"username"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Picture       string `json:"picture"`
	Country       string `json:"country,omitempty"`

	PhoneNumberVerified bool `json:"phone_number_verified"`

	MFAEnabled bool `json:"mfa_enabled"`
	SSOEnabled bool `json:"sso_enabled"`

	ConsentMethod string    `json:"consent_method,omitempty"`
	ConsentedAt   time.Time `json:"consented_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Backup struct {
	ID            string    `json:"id"`
	EnvironmentID string    `json:"environment"`
	Status        string    `json:"status"`
	Safe          bool      `json:"safe"`
	Restorable    bool      `json:"restorable"`
	Automated     bool      `json:"automated"`
	CommitID      string    `json:"commit_id"`
	ExpiresAt     time.Time `json:"expires_at"`

	Links HalLinks `json:"_links,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Activity struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters"`

	State             string    `json:"state"`
	Result            string    `json:"result"`
	CompletionPercent int       `json:"completion_percent"`
	CompletedAt       time.Time `json:"completed_at"`
	StartedAt         time.Time `json:"started_at"`

	Project      string   `json:"project"`
	Environments []string `json:"environments"`
	Integration  string   `json:"integration,omitempty"`

	Description string `json:"description"`
	Text        string `json:"text"`

	Payload any `json:"payload"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Links defaults to "self", "log" and, if the activity is cancellable, "#cancel".
	Links HalLinks `json:"_links,omitempty"`

	// pendingResult is the result the activity completes with when it is next read.
	pendingResult string
}

// MarshalJSON adds the default links. Its value receiver means it also applies when encoding a value.
func (a Activity) MarshalJSON() ([]byte, error) { //nolint:gocritic // See above.
	type activity Activity
	c := activity(a)
	if c.Payload == nil {
		c.Payload = map[string]any{}
	}
	if c.Parameters == nil {
		c.Parameters = map[string]any{}
	}
	if c.Links == nil {
		path := "/projects/" + url.PathEscape(a.Project) + "/activities/" + url.PathEscape(a.ID)
		c.Links = MakeHALLinks("self="+path, "log="+path+"/log")
		if a.cancellable() {
			c.Links["#cancel"] = HALLink{HREF: path + "/cancel"}
		}
	}
	// An activity has no result until it completes.
	var result *string
	if c.Result != "" {
		result = &c.Result
	}
	return json.Marshal(struct {
		activity
		Result *string `json:"result"`
	}{c, result})
}

func (a *Activity) cancellable() bool {
	return a.State == "pending" || a.State == "in_progress"
}

type Domain struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	RegisteredName string    `json:"registered_name,omitempty"`
	Type           string    `json:"type"`
	IsDefault      bool      `json:"is_default"`
	ReplacementFor string    `json:"replacement_for,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	SSL *DomainSSL `json:"ssl"`

	Links HalLinks `json:"_links,omitempty"`
}

// MarshalJSON leaves out is_default from replacement domains, as the API does.
func (d Domain) MarshalJSON() ([]byte, error) { //nolint:gocritic // So that it applies to values too.
	type domain Domain
	b, err := json.Marshal(domain(d))
	if err != nil || d.ReplacementFor == "" {
		return b, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	delete(m, "is_default")
	return json.Marshal(m)
}

// DomainSSL is computed from the project's certificates, as the API does.
type DomainSSL struct {
	HasCertificate bool       `json:"has_certificate"`
	Certificate    string     `json:"certificate,omitempty"`
	ExpiresOn      *time.Time `json:"expires_on,omitempty"`
}

type Integration struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	BaseURL    string `json:"base_url,omitempty"`
	Token      string `json:"-"` // Write-only in the API.
	Repository string `json:"repository,omitempty"`

	BuildPullRequests bool `json:"build_pull_requests,omitempty"`

	URL    string   `json:"url,omitempty"`
	Events []string `json:"events,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Links HalLinks `json:"_links,omitempty"`
}

type Certificate struct {
	ID            string     `json:"id"`
	Certificate   string     `json:"certificate"`
	Chain         []string   `json:"chain"`
	Domains       []string   `json:"domains"`
	AuthType      []string   `json:"auth_type"`
	Issuer        []NameAttr `json:"issuer"`
	IsProvisioned bool       `json:"is_provisioned"`
	IsInvalid     bool       `json:"is_invalid"`
	IsRoot        bool       `json:"is_root"`

	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Links HalLinks `json:"_links,omitempty"`
}

// MarshalJSON outputs empty lists instead of null.
func (c Certificate) MarshalJSON() ([]byte, error) { //nolint:gocritic // So that it applies to values too.
	type certificate Certificate
	v := certificate(c)
	if v.AuthType == nil {
		v.AuthType = []string{}
	}
	if v.Chain == nil {
		v.Chain = []string{}
	}
	if v.Domains == nil {
		v.Domains = []string{}
	}
	if v.Issuer == nil {
		v.Issuer = []NameAttr{}
	}
	return json.Marshal(v)
}

// NameAttr is an attribute of an X.509 name, such as the certificate issuer.
type NameAttr struct {
	OID   string `json:"oid"`
	Alias string `json:"alias"`
	Value string `json:"value"`
}

type Variable struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Value            string            `json:"value,omitempty"`
	Attributes       map[string]string `json:"attributes"`
	IsJSON           bool              `json:"is_json"`
	IsSensitive      bool              `json:"is_sensitive"`
	VisibleBuild     bool              `json:"visible_build"`
	VisibleRuntime   bool              `json:"visible_runtime"`
	ApplicationScope []string          `json:"application_scope"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Links HalLinks `json:"_links,omitempty"`
}

type EnvLevelVariable struct {
	Variable

	Project       string `json:"project"`
	Environment   string `json:"environment"`
	IsEnabled     bool   `json:"is_enabled"`
	Inherited     bool   `json:"inherited"`
	IsInheritable bool   `json:"is_inheritable"`
}

// BillingProfile is a billing profile on the new billing system.
type BillingProfile struct {
	ID                 string `json:"billing_profile_id"`
	Name               string `json:"name"`
	BillingEmail       string `json:"billing_email"`
	BillingContacts    string `json:"billing_contacts"`
	Country            string `json:"country"`
	Currency           string `json:"currency"`
	BillingStreet1     string `json:"billing_street_1"`
	BillingStreet2     string `json:"billing_street_2"`
	Locality           string `json:"locality"`
	AdministrativeArea string `json:"administrative_area"`
	PostalCode         string `json:"postal_code"`
	PricingModel       string `json:"pricing_model"`

	Links HalLinks `json:"_links,omitempty"`
}
