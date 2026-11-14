package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadAuth(t *testing.T, body string) (*Config, error) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "krate.config.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return LoadWith(root, LoadOptions{Command: "build", Mode: "production"})
}

func TestAuthConfigLoads(t *testing.T) {
	cfg, err := loadAuth(t, `{"auth":{
		"basePath":"/api/auth",
		"providers":[{"id":"github"},{"id":"google","options":{"scope":"openid email"}}],
		"session":{"strategy":"database","maxAge":3600},
		"adapter":{"dialect":"sqlite","tables":{"users":"app_users"}},
		"twoFactor":{"enabled":true,"issuer":"Krate","digits":6,"period":30,"algorithm":"sha1","backupCodes":8},
		"passkeys":{"enabled":true,"rpName":"Krate","rpId":"example.com","origins":["https://example.com"]},
		"protect":[{"source":"/dashboard","redirect":"/login"},{"source":"/api/admin","status":403}]
	}}`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a := cfg.Auth
	if !a.Active() || len(a.Providers) != 2 || a.Providers[1].Options["scope"] != "openid email" {
		t.Fatalf("auth not parsed: %+v", a)
	}
	if a.StrategyOrDefault() != "database" || a.MaxAgeOrDefault() != 3600 {
		t.Errorf("session: %s %d", a.StrategyOrDefault(), a.MaxAgeOrDefault())
	}
	if a.Adapter.Tables["users"] != "app_users" || !a.TwoFactor.Enabled || !a.Passkeys.Enabled {
		t.Errorf("nested blocks lost: %+v", a)
	}
	raw, err := a.RuntimeJSON()
	if err != nil || !json.Valid(raw) || !strings.Contains(string(raw), `"github"`) {
		t.Errorf("RuntimeJSON = %s, %v", raw, err)
	}
}

func TestAuthDefaults(t *testing.T) {
	var a AuthConfig
	if a.Active() {
		t.Error("empty auth must be inactive")
	}
	if a.BasePathOrDefault() != "/api/auth" || a.StrategyOrDefault() != "jwt" || a.MaxAgeOrDefault() != 2592000 {
		t.Errorf("defaults: %s %s %d", a.BasePathOrDefault(), a.StrategyOrDefault(), a.MaxAgeOrDefault())
	}
	off := false
	a = AuthConfig{Enabled: &off, Providers: []AuthProvider{{ID: "github"}}}
	if a.Active() {
		t.Error("enabled:false must win over providers")
	}
	a = AuthConfig{Adapter: AuthAdapter{Dialect: "postgres"}}
	if a.StrategyOrDefault() != "database" {
		t.Error("an adapter implies the database strategy")
	}
}

func TestAuthValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		auth string
		want string
	}{
		{"base path", `{"basePath":"auth"}`, "auth.basePath"},
		{"trailing slash", `{"basePath":"/auth/","providers":[{"id":"github"}]}`, "must not end with /"},
		{"strategy", `{"providers":[{"id":"github"}],"session":{"strategy":"cookie"}}`, "auth.session.strategy"},
		{"database without adapter", `{"providers":[{"id":"github"}],"session":{"strategy":"database"}}`, "auth.adapter.dialect"},
		{"2fa without adapter", `{"twoFactor":{"enabled":true}}`, "auth.twoFactor needs"},
		{"passkeys without adapter", `{"passkeys":{"enabled":true}}`, "auth.passkeys needs"},
		{"dialect", `{"providers":[{"id":"github"}],"adapter":{"dialect":"mongo"}}`, "auth.adapter.dialect"},
		{"table", `{"providers":[{"id":"github"}],"adapter":{"dialect":"sqlite","tables":{"bogus":"x"}}}`, "auth.adapter.tables.bogus"},
		{"samesite", `{"providers":[{"id":"github"}],"cookies":{"sameSite":"wild"}}`, "auth.cookies.sameSite"},
		{"samesite none insecure", `{"providers":[{"id":"github"}],"cookies":{"sameSite":"none","secure":false}}`, "requires secure"},
		{"missing id", `{"providers":[{"name":"x"}]}`, "needs an id"},
		{"bad id", `{"providers":[{"id":"Git Hub"}]}`, "lowercase"},
		{"duplicate id", `{"providers":[{"id":"github"},{"id":"github"}]}`, "listed twice"},
		{"bad type", `{"providers":[{"id":"x","type":"saml"}]}`, "type must be one of"},
		{"secret in options", `{"providers":[{"id":"github","options":{"clientSecret":"abc"}}]}`, "AUTH_GITHUB_SECRET"},
		{"nested secret", `{"providers":[{"id":"okta","options":{"extra":{"apiKey":"abc"}}}]}`, "options.extra.apiKey"},
		{"digits", `{"twoFactor":{"enabled":true,"digits":4},"adapter":{"dialect":"sqlite"}}`, "digits"},
		{"algorithm", `{"twoFactor":{"enabled":true,"algorithm":"MD5"},"adapter":{"dialect":"sqlite"}}`, "algorithm"},
		{"backup codes", `{"twoFactor":{"enabled":true,"backupCodes":99},"adapter":{"dialect":"sqlite"}}`, "backupCodes"},
		{"user verification", `{"passkeys":{"enabled":true,"userVerification":"maybe"},"adapter":{"dialect":"sqlite"}}`, "userVerification"},
		{"attestation", `{"passkeys":{"enabled":true,"attestation":"x"},"adapter":{"dialect":"sqlite"}}`, "attestation"},
		{"rp id", `{"passkeys":{"enabled":true,"rpId":"https://example.com"},"adapter":{"dialect":"sqlite"}}`, "rpId"},
		{"insecure origin", `{"passkeys":{"enabled":true,"origins":["http://example.com"]},"adapter":{"dialect":"sqlite"}}`, "origins"},
		{"protect source", `{"providers":[{"id":"github"}],"protect":[{"source":"dashboard"}]}`, "protect[0].source"},
		{"protect redirect", `{"providers":[{"id":"github"}],"protect":[{"source":"/a","redirect":"login"}]}`, "protect[0].redirect"},
		{"protect status", `{"providers":[{"id":"github"}],"protect":[{"source":"/a","status":500}]}`, "protect[0].status"},
		{"redirects", `{"providers":[{"id":"github"}],"redirects":{"signedIn":"home"}}`, "auth.redirects"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadAuth(t, `{"auth":`+tc.auth+`}`)
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestAuthWarnings(t *testing.T) {
	c := &Config{Auth: AuthConfig{
		Providers: []AuthProvider{{ID: "acme-sso"}, {ID: "okta"}},
		Session:   AuthSession{MaxAge: 60, UpdateAge: 120},
		TwoFactor: AuthTwoFactor{Required: true},
	}}
	warnings, err := c.Auth.validate()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"acme-sso", "updateAge", "twoFactor.required"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning about %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `"okta"`) {
		t.Errorf("okta is built in and must not warn:\n%s", joined)
	}
}

func TestAuthCustomProviderTypeSilencesWarning(t *testing.T) {
	a := AuthConfig{Providers: []AuthProvider{{ID: "acme-sso", Type: "oidc", Options: map[string]any{"issuer": "https://sso.acme.test"}}}}
	warnings, err := a.validate()
	if err != nil || len(warnings) != 0 {
		t.Fatalf("warnings=%v err=%v", warnings, err)
	}
}

func TestAuthUnknownKeysWarn(t *testing.T) {
	raw := []byte(`{"auth":{"providers":[{"id":"github","optons":{}}],"sesion":{},"twoFactor":{"enable":true}}}`)
	got := strings.Join(UnknownKeyWarnings(raw), "\n")
	for _, want := range []string{"auth.sesion", "auth.twoFactor.enable", "auth.providers.optons"} {
		if !strings.Contains(got, want) {
			t.Errorf("no unknown-key warning for %s in:\n%s", want, got)
		}
	}
}

func TestAuthSchemaIncludesAuth(t *testing.T) {
	raw, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"twoFactor"`) || !strings.Contains(string(raw), `"passkeys"`) {
		t.Error("schema is missing the auth block")
	}
}
