package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type AuthConfig struct {
	Enabled   *bool            `json:"enabled,omitempty"`
	BasePath  string           `json:"basePath,omitempty"`
	TrustHost *bool            `json:"trustHost,omitempty"`
	Debug     bool             `json:"debug,omitempty"`
	Providers []AuthProvider   `json:"providers,omitempty"`
	Session   AuthSession      `json:"session,omitempty"`
	Pages     AuthPages        `json:"pages,omitempty"`
	Cookies   AuthCookies      `json:"cookies,omitempty"`
	Adapter   AuthAdapter      `json:"adapter,omitempty"`
	TwoFactor AuthTwoFactor    `json:"twoFactor,omitempty"`
	Passkeys  AuthPasskeys     `json:"passkeys,omitempty"`
	Protect   []AuthProtect    `json:"protect,omitempty"`
	RateLimit AuthRateLimit    `json:"rateLimit,omitempty"`
	Redirects AuthRedirects    `json:"redirects,omitempty"`
	Linking   AuthAccountLinks `json:"linking,omitempty"`
}

type AuthProvider struct {
	ID      string         `json:"id"`
	Type    string         `json:"type,omitempty"`
	Name    string         `json:"name,omitempty"`
	Enabled *bool          `json:"enabled,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

type AuthSession struct {
	Strategy  string `json:"strategy,omitempty"`
	MaxAge    int    `json:"maxAge,omitempty"`
	UpdateAge int    `json:"updateAge,omitempty"`
}

type AuthPages struct {
	SignIn        string `json:"signIn,omitempty"`
	SignOut       string `json:"signOut,omitempty"`
	Error         string `json:"error,omitempty"`
	VerifyRequest string `json:"verifyRequest,omitempty"`
	NewUser       string `json:"newUser,omitempty"`
	TwoFactor     string `json:"twoFactor,omitempty"`
}

type AuthCookies struct {
	Prefix   string `json:"prefix,omitempty"`
	Secure   *bool  `json:"secure,omitempty"`
	SameSite string `json:"sameSite,omitempty"`
	Domain   string `json:"domain,omitempty"`
}

type AuthAdapter struct {
	Dialect string            `json:"dialect,omitempty"`
	Tables  map[string]string `json:"tables,omitempty"`
}

type AuthTwoFactor struct {
	Enabled     bool   `json:"enabled,omitempty"`
	Issuer      string `json:"issuer,omitempty"`
	Digits      int    `json:"digits,omitempty"`
	Period      int    `json:"period,omitempty"`
	Algorithm   string `json:"algorithm,omitempty"`
	Window      int    `json:"window,omitempty"`
	BackupCodes int    `json:"backupCodes,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type AuthPasskeys struct {
	Enabled          bool     `json:"enabled,omitempty"`
	AllowSignUp      *bool    `json:"allowSignUp,omitempty"`
	RPName           string   `json:"rpName,omitempty"`
	RPID             string   `json:"rpId,omitempty"`
	Origins          []string `json:"origins,omitempty"`
	UserVerification string   `json:"userVerification,omitempty"`
	ResidentKey      string   `json:"residentKey,omitempty"`
	Attestation      string   `json:"attestation,omitempty"`
	Timeout          int      `json:"timeout,omitempty"`
}

type AuthProtect struct {
	Source   string   `json:"source"`
	Roles    []string `json:"roles,omitempty"`
	Redirect string   `json:"redirect,omitempty"`
	Status   int      `json:"status,omitempty"`
}

type AuthRateLimit struct {
	Enabled *bool `json:"enabled,omitempty"`
	Max     int   `json:"max,omitempty"`
	Window  int   `json:"window,omitempty"`
}

type AuthRedirects struct {
	SignedIn  string   `json:"signedIn,omitempty"`
	SignedOut string   `json:"signedOut,omitempty"`
	Allowed   []string `json:"allowed,omitempty"`
}

type AuthAccountLinks struct {
	Automatic bool `json:"automatic,omitempty"`
}

var AuthKnownProviders = []string{
	"42-school", "apple", "asgardeo", "atlassian", "auth0", "authentik", "azure-ad-b2c",
	"bitbucket", "box", "cognito", "coinbase", "descope", "discord", "dribbble", "dropbox",
	"duende-identityserver6", "facebook", "fusionauth", "github", "gitlab", "google", "hubspot",
	"huggingface", "kakao", "keycloak", "kinde", "line", "linkedin", "logto", "mastodon",
	"mattermost", "microsoft-entra-id", "naver", "netlify", "notion", "okta", "onelogin", "orcid",
	"osu", "patreon", "pinterest", "reddit", "roblox", "salesforce", "slack", "spotify", "strava",
	"twitch", "twitter", "webex", "wikimedia", "workos", "yandex", "zitadel", "zoho", "zoom",
	"credentials", "email", "resend", "passkey",
}

var authProviderTypes = map[string]bool{
	"oauth": true, "oidc": true, "credentials": true, "email": true, "passkey": true,
}

var authKnownProviderSet = func() map[string]bool {
	m := make(map[string]bool, len(AuthKnownProviders))
	for _, id := range AuthKnownProviders {
		m[id] = true
	}
	return m
}()

var authSecretKeys = map[string]bool{
	"clientsecret": true, "secret": true, "password": true, "apikey": true,
	"privatekey": true, "token": true, "accesstoken": true, "authsecret": true,
}

var authIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (a AuthConfig) Active() bool {
	if a.Enabled != nil {
		return *a.Enabled
	}
	return len(a.Providers) > 0 || a.TwoFactor.Enabled || a.Passkeys.Enabled
}

func (a AuthConfig) BasePathOrDefault() string {
	if a.BasePath == "" {
		return "/api/auth"
	}
	return a.BasePath
}

func (a AuthConfig) StrategyOrDefault() string {
	if a.Session.Strategy != "" {
		return a.Session.Strategy
	}
	if a.Adapter.Dialect != "" {
		return "database"
	}
	return "jwt"
}

func (a AuthConfig) MaxAgeOrDefault() int {
	if a.Session.MaxAge > 0 {
		return a.Session.MaxAge
	}
	return 30 * 24 * 60 * 60
}

func (a AuthConfig) NeedsAdapter() bool {
	return a.StrategyOrDefault() == "database" || a.TwoFactor.Enabled || a.Passkeys.Enabled
}

func (a AuthConfig) ProviderEnabled(p AuthProvider) bool {
	return p.Enabled == nil || *p.Enabled
}

func (a AuthConfig) RuntimeJSON() ([]byte, error) {
	return json.Marshal(a)
}

func authContains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func authOneOf(v string, allowed ...string) bool {
	return v == "" || authContains(allowed, v)
}

func findSecretKey(v any, path string) (string, bool) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := path + "." + k
			if authSecretKeys[strings.ToLower(k)] {
				return p, true
			}
			if found, ok := findSecretKey(t[k], p); ok {
				return found, true
			}
		}
	case []any:
		for i, item := range t {
			if found, ok := findSecretKey(item, fmt.Sprintf("%s[%d]", path, i)); ok {
				return found, true
			}
		}
	}
	return "", false
}

func (a AuthConfig) validate() (warnings []string, err error) {
	if bp := a.BasePath; bp != "" {
		if !strings.HasPrefix(bp, "/") || strings.Contains(bp, "://") {
			return nil, fmt.Errorf("auth.basePath must be a path starting with / (e.g. \"/api/auth\"), got %q", bp)
		}
		if bp != "/" && strings.HasSuffix(bp, "/") {
			return nil, fmt.Errorf("auth.basePath must not end with /, got %q", bp)
		}
	}
	if !authOneOf(a.Session.Strategy, "jwt", "database") {
		return nil, fmt.Errorf("auth.session.strategy must be \"jwt\" or \"database\", got %q", a.Session.Strategy)
	}
	if a.Session.MaxAge < 0 || a.Session.UpdateAge < 0 {
		return nil, fmt.Errorf("auth.session.maxAge and auth.session.updateAge must be >= 0 seconds")
	}
	if a.Session.UpdateAge > 0 && a.Session.MaxAge > 0 && a.Session.UpdateAge > a.Session.MaxAge {
		warnings = append(warnings, "auth.session.updateAge is larger than maxAge, so the session is never extended")
	}
	if !authOneOf(a.Adapter.Dialect, "sqlite", "postgres", "mysql") {
		return nil, fmt.Errorf("auth.adapter.dialect must be \"sqlite\", \"postgres\" or \"mysql\", got %q", a.Adapter.Dialect)
	}
	for k := range a.Adapter.Tables {
		switch k {
		case "users", "accounts", "sessions", "verificationTokens", "authenticators", "twoFactors":
		default:
			return nil, fmt.Errorf("auth.adapter.tables.%s is not a known table (users, accounts, sessions, verificationTokens, authenticators, twoFactors)", k)
		}
	}
	if a.NeedsAdapter() && a.Adapter.Dialect == "" {
		switch {
		case a.Session.Strategy == "database":
			return nil, fmt.Errorf("auth.session.strategy \"database\" needs auth.adapter.dialect (sqlite, postgres or mysql)")
		case a.TwoFactor.Enabled:
			return nil, fmt.Errorf("auth.twoFactor needs auth.adapter.dialect to store secrets and backup codes")
		default:
			return nil, fmt.Errorf("auth.passkeys needs auth.adapter.dialect to store credentials")
		}
	}
	switch a.Cookies.SameSite {
	case "", "lax", "strict", "none":
	default:
		return nil, fmt.Errorf("auth.cookies.sameSite must be \"lax\", \"strict\" or \"none\", got %q", a.Cookies.SameSite)
	}
	if a.Cookies.SameSite == "none" && a.Cookies.Secure != nil && !*a.Cookies.Secure {
		return nil, fmt.Errorf("auth.cookies.sameSite \"none\" requires secure cookies; browsers drop the cookie otherwise")
	}
	if strings.ContainsAny(a.Cookies.Prefix, " ;=,\t") {
		return nil, fmt.Errorf("auth.cookies.prefix %q contains characters that are not valid in a cookie name", a.Cookies.Prefix)
	}

	seen := map[string]bool{}
	for i, p := range a.Providers {
		where := fmt.Sprintf("auth.providers[%d]", i)
		if p.ID == "" {
			return nil, fmt.Errorf("%s needs an id", where)
		}
		if !authIDPattern.MatchString(p.ID) {
			return nil, fmt.Errorf("%s id %q must be lowercase letters, digits, - or _", where, p.ID)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("%s id %q is listed twice", where, p.ID)
		}
		seen[p.ID] = true
		if p.Type != "" && !authProviderTypes[p.Type] {
			return nil, fmt.Errorf("%s type must be one of oauth, oidc, credentials, email, passkey, got %q", where, p.Type)
		}
		if p.Type == "" && !authKnownProviderSet[p.ID] {
			warnings = append(warnings, fmt.Sprintf("%s id %q is not a built-in provider; set type to \"oauth\" or \"oidc\" with endpoints in options", where, p.ID))
		}
		if found, ok := findSecretKey(p.Options, where+".options"); ok {
			return nil, fmt.Errorf("%s: %s would put a secret in the compiled bundle; set it with an environment variable such as AUTH_%s_SECRET instead", where, found, strings.ToUpper(strings.ReplaceAll(p.ID, "-", "_")))
		}
	}

	tf := a.TwoFactor
	if tf.Digits != 0 && (tf.Digits < 6 || tf.Digits > 8) {
		return nil, fmt.Errorf("auth.twoFactor.digits must be 6, 7 or 8, got %d", tf.Digits)
	}
	if tf.Period < 0 || tf.Window < 0 || tf.Window > 10 {
		return nil, fmt.Errorf("auth.twoFactor.period must be > 0 seconds and window between 0 and 10")
	}
	if !authOneOf(strings.ToUpper(tf.Algorithm), "SHA1", "SHA256", "SHA512") {
		return nil, fmt.Errorf("auth.twoFactor.algorithm must be SHA1, SHA256 or SHA512, got %q", tf.Algorithm)
	}
	if tf.BackupCodes < 0 || tf.BackupCodes > 32 {
		return nil, fmt.Errorf("auth.twoFactor.backupCodes must be between 0 and 32, got %d", tf.BackupCodes)
	}
	if tf.Required && !tf.Enabled {
		warnings = append(warnings, "auth.twoFactor.required has no effect while auth.twoFactor.enabled is false")
	}

	pk := a.Passkeys
	if !authOneOf(pk.UserVerification, "required", "preferred", "discouraged") {
		return nil, fmt.Errorf("auth.passkeys.userVerification must be required, preferred or discouraged, got %q", pk.UserVerification)
	}
	if !authOneOf(pk.ResidentKey, "required", "preferred", "discouraged") {
		return nil, fmt.Errorf("auth.passkeys.residentKey must be required, preferred or discouraged, got %q", pk.ResidentKey)
	}
	if !authOneOf(pk.Attestation, "none", "indirect", "direct", "enterprise") {
		return nil, fmt.Errorf("auth.passkeys.attestation must be none, indirect, direct or enterprise, got %q", pk.Attestation)
	}
	if strings.Contains(pk.RPID, "://") || strings.Contains(pk.RPID, "/") || strings.Contains(pk.RPID, ":") {
		return nil, fmt.Errorf("auth.passkeys.rpId must be a bare domain such as \"example.com\", got %q", pk.RPID)
	}
	for _, o := range pk.Origins {
		if !strings.HasPrefix(o, "https://") && !strings.HasPrefix(o, "http://localhost") && !strings.HasPrefix(o, "http://127.0.0.1") {
			return nil, fmt.Errorf("auth.passkeys.origins entry %q must be an https:// origin (http is allowed for localhost only)", o)
		}
	}
	if pk.Timeout < 0 {
		return nil, fmt.Errorf("auth.passkeys.timeout must be >= 0 milliseconds")
	}

	for i, r := range a.Protect {
		if !strings.HasPrefix(r.Source, "/") {
			return nil, fmt.Errorf("auth.protect[%d].source must start with /, got %q", i, r.Source)
		}
		if r.Redirect != "" && !strings.HasPrefix(r.Redirect, "/") {
			return nil, fmt.Errorf("auth.protect[%d].redirect must be a path starting with /, got %q", i, r.Redirect)
		}
		if r.Status != 0 && r.Status != 401 && r.Status != 403 && r.Status != 404 {
			return nil, fmt.Errorf("auth.protect[%d].status must be 401, 403 or 404, got %d", i, r.Status)
		}
		if r.Redirect != "" && r.Status != 0 {
			warnings = append(warnings, fmt.Sprintf("auth.protect[%d] sets both redirect and status; redirect wins for page requests", i))
		}
	}
	if a.RateLimit.Max < 0 || a.RateLimit.Window < 0 {
		return nil, fmt.Errorf("auth.rateLimit.max and auth.rateLimit.window must be >= 0")
	}
	for _, p := range []string{a.Redirects.SignedIn, a.Redirects.SignedOut} {
		if p != "" && !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("auth.redirects values must be paths starting with /, got %q", p)
		}
	}
	return warnings, nil
}
