package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSecret = "s3cret-test-token"

// noRedirectClient stops at 3xx so tests can inspect the auth cookie set on the
// token-login redirect.
var noRedirectClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// newTestServer stands up a web handler against a fake mihomo controller.
func newTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	ctrl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"path":"`+r.URL.Path+`"}`)
	}))
	uiDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(uiDir, "index.html"), []byte("<html>panel</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	addr, err := url.Parse(ctrl.URL)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		UIDir:          uiDir,
		ControllerAddr: addr.Host,
		Secret:         testSecret,
	}
	ts := httptest.NewServer(srv.handler())
	return ts, func() { ts.Close(); ctrl.Close() }
}

func do(t *testing.T, target string, cookie *http.Cookie, bearer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestGateUnauthedRootShowsLogin(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	resp := do(t, ts.URL+"/", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `name="password"`) {
		t.Fatalf("expected login page, got: %s", b)
	}
}

func TestGateUnauthedAPIIs401(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	resp := do(t, ts.URL+"/proxies", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate header")
	}
}

func TestGateTokenGrantsCookieAndRedirects(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	resp := do(t, ts.URL+"/?token="+testSecret, nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("status=%d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "token=") {
		t.Fatalf("token not stripped from redirect: %s", loc)
	}
	var c *http.Cookie
	for _, k := range resp.Cookies() {
		if k.Name == authCookie {
			c = k
		}
	}
	if c == nil || c.Value != testSecret {
		t.Fatalf("expected %s cookie, got %+v", authCookie, resp.Cookies())
	}
}

func TestGateCookiePassesAndProxies(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	resp := do(t, ts.URL+"/proxies", &http.Cookie{Name: authCookie, Value: testSecret}, "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"path":"/proxies"`) {
		t.Fatalf("request was not proxied to the controller: %s", b)
	}
}

func TestGateBearerPasses(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	resp := do(t, ts.URL+"/version", nil, testSecret)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}

func TestGateBadTokenRejected(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	resp := do(t, ts.URL+"/?token=wrong", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200 (login page)", resp.StatusCode)
	}
	for _, k := range resp.Cookies() {
		if k.Name == authCookie {
			t.Fatal("must not grant auth cookie for a wrong token")
		}
	}
}

func TestGateLoginWrongPasswordRejected(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+authLoginPath, strings.NewReader("password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
	for _, k := range resp.Cookies() {
		if k.Name == authCookie {
			t.Fatal("must not grant auth cookie for a wrong password")
		}
	}
}

func TestGateLoginCorrectGrantsCookie(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+authLoginPath, strings.NewReader("password="+testSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("status=%d, want 302", resp.StatusCode)
	}
	var got bool
	for _, k := range resp.Cookies() {
		if k.Name == authCookie && k.Value == testSecret {
			got = true
		}
	}
	if !got {
		t.Fatalf("expected %s cookie on correct login", authCookie)
	}
}
