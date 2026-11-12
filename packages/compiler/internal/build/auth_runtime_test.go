package build

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

const authTestSecret = "embedded-runtime-secret-with-plenty-of-entropy-0123456789"

func authDistDir(t *testing.T) string {
	t.Helper()
	dist, err := filepath.Abs(filepath.Join("..", "..", "..", "auth", "dist"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dist, "index.js")); err != nil {
		requireE2E(t, "@krate/auth is not built (run pnpm --filter @krate/auth build): %v", err)
		return ""
	}
	return dist
}

func authTOTP(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decoding the TOTP secret: %v", err)
	}
	mac := hmac.New(sha1.New, key)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30))
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[off]&0x7f) << 24) | (uint32(sum[off+1]) << 16) | (uint32(sum[off+2]) << 8) | uint32(sum[off+3])
	return fmt.Sprintf("%06d", code%1000000)
}

type authHTTP struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newAuthHTTP(t *testing.T, base string) *authHTTP {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &authHTTP{t: t, base: base, client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (a *authHTTP) do(method, path, contentType, body string) (*http.Response, string) {
	a.t.Helper()
	req, err := http.NewRequest(method, a.base+path, strings.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := a.client.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, string(data)
}

func (a *authHTTP) csrf() string {
	a.t.Helper()
	_, body := a.do("GET", "/api/auth/csrf", "", "")
	var out struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.CSRFToken == "" {
		a.t.Fatalf("csrf response %q: %v", body, err)
	}
	return out.CSRFToken
}

func (a *authHTTP) postForm(path string, fields map[string]string) (*http.Response, string) {
	a.t.Helper()
	form := url.Values{"csrfToken": {a.csrf()}}
	for k, v := range fields {
		form.Set(k, v)
	}
	return a.do("POST", path, "application/x-www-form-urlencoded", form.Encode())
}

func (a *authHTTP) postJSON(path string, fields map[string]any) (*http.Response, map[string]any) {
	a.t.Helper()
	payload := map[string]any{"csrfToken": a.csrf()}
	for k, v := range fields {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	res, body := a.do("POST", path, "application/json", string(raw))
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	return res, out
}

func (a *authHTTP) hasCookie(name string) bool {
	u, _ := url.Parse(a.base)
	for _, c := range a.client.Jar.Cookies(u) {
		if c.Name == name && c.Value != "" {
			return true
		}
	}
	return false
}

func TestAuthPackageInEmbeddedRuntime(t *testing.T) {
	dist := authDistDir(t)
	if dist == "" {
		return
	}
	imp := filepath.ToSlash(dist)
	fixtures, err := os.ReadFile(filepath.Join("testdata", "auth_webauthn_fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}

	b, root := compatProject(t, map[string]string{
		"src/auth.ts": `import { defineAuth, credentials, MemoryAdapter } from "` + imp + `/index.js";
const adapter = MemoryAdapter();
export const auth = defineAuth({
  adapter,
  session: { strategy: "jwt" },
  providers: [
    credentials({
      authorize: async (c) => {
        if (c.password !== "pw") return null;
        return (await adapter.getUserByEmail(c.email)) ?? (await adapter.createUser({ email: c.email, name: "Ada", emailVerified: null }));
      },
    }),
  ],
});
`,
		"src/api/auth/[...all].ts": `import { auth } from "../../auth";
export const GET = auth.handlers.GET;
export const POST = auth.handlers.POST;
`,
		"src/api/me.ts": `import { auth } from "../auth";
export async function GET(request: Request) {
  return Response.json({ session: await auth.auth(request) });
}
`,
		"src/api/webauthn.ts": `import { verifyRegistration, verifyAuthentication } from "` + imp + `/webauthn.js";
const fixtures = ` + string(fixtures) + `;
export function GET() {
  const results = fixtures.cases.map((c: any) => {
    const common = { origins: [fixtures.origin], rpId: fixtures.rpId, requireUserVerification: true };
    const reg = verifyRegistration({ credential: c.registration, challenge: c.regChallenge, ...common });
    let forgedRejected = false;
    try {
      verifyAuthentication({ credential: c.forged, challenge: c.authChallenge, publicKey: reg.publicKey, storedCounter: 0, ...common });
    } catch {
      forgedRejected = true;
    }
    const auth = verifyAuthentication({ credential: c.authentication, challenge: c.authChallenge, publicKey: reg.publicKey, storedCounter: 0, ...common });
    return { name: c.name, credentialID: reg.credentialID, counter: auth.counter, userVerified: auth.userVerified, forgedRejected };
  });
  return Response.json(results);
}
`,
	}, func(c *config.Config) {
		c.Auth = config.AuthConfig{
			Providers: []config.AuthProvider{{ID: "github"}},
			Pages:     config.AuthPages{SignIn: "/login"},
			TwoFactor: config.AuthTwoFactor{Enabled: true, Issuer: "Embedded"},
		}
	})
	var routes []string
	for _, rel := range []string{"src/api/auth/[...all].ts", "src/api/me.ts", "src/api/webauthn.ts"} {
		routes = append(routes, filepath.Join(root, filepath.FromSlash(rel)))
	}
	if err := b.CompileAPIRoutes(routes); err != nil {
		t.Fatalf("compiling the auth routes: %v", err)
	}
	for file, meta := range readRouteMetas(t, root) {
		if meta.NodeOnly {
			t.Errorf("route %s was marked Node-only; @krate/auth must stay on the embedded runtime", file)
		}
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) > 0 {
		t.Errorf("@krate/auth uses APIs the embedded runtime lacks: %v", ws)
	}

	rt := jsruntime.NewAPIRouteRuntime(apiBundleDir(b.Cfg.OutDir))
	rt.SetEnv(map[string]string{"AUTH_SECRET": authTestSecret})
	rt.SetPool(1, false)
	t.Cleanup(rt.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveQuickJSAPI(w, r, rt, true)
	}))
	t.Cleanup(srv.Close)

	t.Run("config reaches the package", func(t *testing.T) {
		a := newAuthHTTP(t, srv.URL)
		res, body := a.do("GET", "/api/auth/providers", "", "")
		if res.StatusCode != 200 {
			t.Fatalf("providers: %d %s", res.StatusCode, body)
		}
		var providers map[string]map[string]any
		if err := json.Unmarshal([]byte(body), &providers); err != nil {
			t.Fatal(err)
		}
		if _, ok := providers["github"]; !ok {
			t.Errorf("the github provider declared in krate.config is missing: %s", body)
		}
		if _, ok := providers["credentials"]; !ok {
			t.Errorf("the credentials provider declared in code is missing: %s", body)
		}
		res, _ = a.do("GET", "/api/auth/signin", "", "")
		if res.StatusCode != 302 || !strings.Contains(res.Header.Get("Location"), "/login?callbackUrl=") {
			t.Errorf("pages.signIn from krate.config was not used: %d %q", res.StatusCode, res.Header.Get("Location"))
		}
	})

	t.Run("credentials sign in, session and sign out", func(t *testing.T) {
		a := newAuthHTTP(t, srv.URL)
		res, body := a.postForm("/api/auth/callback/credentials", map[string]string{"email": "ada@example.com", "password": "wrong"})
		if res.StatusCode != 302 || !strings.Contains(res.Header.Get("Location"), "error=CredentialsSignin") || a.hasCookie("krate-auth.session-token") {
			t.Fatalf("bad password: %d %q %s", res.StatusCode, res.Header.Get("Location"), body)
		}
		res, body = a.postForm("/api/auth/callback/credentials", map[string]string{"email": "ada@example.com", "password": "pw", "callbackUrl": "/dash"})
		if res.StatusCode != 302 || res.Header.Get("Location") != srv.URL+"/dash" || !a.hasCookie("krate-auth.session-token") {
			t.Fatalf("sign in: %d %q %s", res.StatusCode, res.Header.Get("Location"), body)
		}
		_, body = a.do("GET", "/api/me", "", "")
		var me struct {
			Session *struct {
				User struct {
					Email string `json:"email"`
					Name  string `json:"name"`
				} `json:"user"`
			} `json:"session"`
		}
		if err := json.Unmarshal([]byte(body), &me); err != nil || me.Session == nil || me.Session.User.Email != "ada@example.com" {
			t.Fatalf("session: %s (%v)", body, err)
		}
		a.postForm("/api/auth/signout", nil)
		_, body = a.do("GET", "/api/me", "", "")
		if !strings.Contains(body, `"session":null`) {
			t.Errorf("session survived sign out: %s", body)
		}
	})

	t.Run("two-factor enrollment and sign in challenge", func(t *testing.T) {
		a := newAuthHTTP(t, srv.URL)
		a.postForm("/api/auth/callback/credentials", map[string]string{"email": "grace@example.com", "password": "pw"})
		res, setup := a.postJSON("/api/auth/2fa/setup", nil)
		secret, _ := setup["secret"].(string)
		if res.StatusCode != 200 || len(secret) != 32 || !strings.Contains(fmt.Sprint(setup["otpauthUrl"]), "Embedded") {
			t.Fatalf("setup: %d %v", res.StatusCode, setup)
		}
		res, enabled := a.postJSON("/api/auth/2fa/enable", map[string]any{"code": authTOTP(t, secret)})
		codes, _ := enabled["backupCodes"].([]any)
		if res.StatusCode != 200 || len(codes) != 10 {
			t.Fatalf("enable: %d %v", res.StatusCode, enabled)
		}

		b := newAuthHTTP(t, srv.URL)
		res, body := b.postForm("/api/auth/callback/credentials", map[string]string{"email": "grace@example.com", "password": "pw", "callbackUrl": "/dash"})
		if res.StatusCode != 302 || !strings.Contains(res.Header.Get("Location"), "/api/auth/2fa?callbackUrl=") || b.hasCookie("krate-auth.session-token") {
			t.Fatalf("challenge: %d %q %s", res.StatusCode, res.Header.Get("Location"), body)
		}
		res, _ = b.postForm("/api/auth/2fa/verify", map[string]string{"code": "AAAAA-AAAAA"})
		if !strings.Contains(res.Header.Get("Location"), "error=InvalidCode") || b.hasCookie("krate-auth.session-token") {
			t.Fatalf("wrong backup code: %d %q", res.StatusCode, res.Header.Get("Location"))
		}
		res, body = b.postForm("/api/auth/2fa/verify", map[string]string{"code": fmt.Sprint(codes[0])})
		if res.StatusCode != 302 || res.Header.Get("Location") != srv.URL+"/dash" || !b.hasCookie("krate-auth.session-token") {
			t.Fatalf("verify: %d %q %s", res.StatusCode, res.Header.Get("Location"), body)
		}
	})

	t.Run("webauthn signatures verify for ES256, EdDSA and RS256", func(t *testing.T) {
		a := newAuthHTTP(t, srv.URL)
		res, body := a.do("GET", "/api/webauthn", "", "")
		if res.StatusCode != 200 {
			t.Fatalf("webauthn: %d %s", res.StatusCode, body)
		}
		var results []struct {
			Name           string `json:"name"`
			CredentialID   string `json:"credentialID"`
			Counter        int    `json:"counter"`
			UserVerified   bool   `json:"userVerified"`
			ForgedRejected bool   `json:"forgedRejected"`
		}
		if err := json.Unmarshal([]byte(body), &results); err != nil {
			t.Fatalf("decoding %s: %v", body, err)
		}
		if len(results) != 3 {
			t.Fatalf("expected three algorithms, got %s", body)
		}
		for _, r := range results {
			if r.CredentialID == "" || r.Counter != 1 || !r.UserVerified || !r.ForgedRejected {
				t.Errorf("%s: %+v", r.Name, r)
			}
		}
	})
}
