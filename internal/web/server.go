// Package web serves the zashboard static panel and reverse-proxies the Clash
// REST API (injecting the secret) so users open one address with no manual setup.
//
// A single secret guards both surfaces: it is written into the mihomo config
// (protecting the kernel's external-controller) AND used as the web panel's
// access credential. Callers prove they know it via a session cookie (granted by
// the login page or a one-shot ?token= URL) or a Bearer header, after which the
// reverse proxy injects it into every proxied API request.
package web

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	// setupCookie breaks the root->setup redirect loop: the server cannot see the
	// URL fragment, so it cannot tell "/#/setup" apart from a bare "/". This
	// short-lived marker lets the immediate follow-up request serve the SPA
	// instead of redirecting again. It intentionally expires within seconds so a
	// later fresh load of "/" re-asserts the same-origin proxy backend, self-
	// healing any stale port zashboard may have fallen back to (its default is
	// 127.0.0.1:9090). Re-running setup is safe: zashboard dedupes backends by
	// host/port/secret, so this never accumulates duplicates.
	setupCookie = "zh_setup"
	// setupCookieTTL is how long the loop-breaker survives. Only long enough to
	// span the redirect it guards; short so every fresh "/" load re-runs setup.
	setupCookieTTL = 10 * time.Second
	// authCookie carries the secret after a successful login; its presence (with
	// the correct value) lets the caller through the secret gate.
	authCookie = "zh_auth"
	// authLoginPath is the always-open endpoint that swaps the secret for a
	// session cookie (GET renders the form, POST validates it).
	authLoginPath = "/__auth"
)

// apiRoots are the first path segments that belong to the Clash REST API and
// must be reverse-proxied to the kernel rather than served as static files.
var apiRoots = map[string]bool{
	"version":     true,
	"configs":     true,
	"proxies":     true,
	"connections": true,
	"rules":       true,
	"providers":   true,
	"logs":        true,
	"traffic":     true,
	"memory":      true,
	"group":       true,
	"dns":         true,
	"cache":       true,
	"restart":     true,
	"upgrade":     true,
	"profile":     true,
	"script":      true,
	"debug":       true,
	"gc":          true,
}

// Server hosts the panel and proxies the API.
type Server struct {
	// Addr is the listen address for the panel (e.g. 127.0.0.1:9191).
	Addr string
	// UIDir is the directory of zashboard static files.
	UIDir string
	// ControllerAddr is the kernel controller (e.g. 127.0.0.1:9090).
	ControllerAddr string
	// Secret guards both the web panel (as the login credential) and the Clash
	// API (written into the mihomo config and injected by the reverse proxy).
	Secret string

	httpServer *http.Server
}

// handler builds the request pipeline: a secret gate wraps the inner mux
// (static panel + reverse-proxied Clash API).
func (s *Server) handler() http.Handler {
	target := &url.URL{Scheme: "http", Host: s.ControllerAddr}
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	secret := s.Secret
	proxy.Director = func(r *http.Request) {
		baseDirector(r)
		// Overwrite any client-supplied credentials with the real secret so the
		// panel never needs to know it (and the gate credential == API secret).
		if secret != "" {
			r.Header.Set("Authorization", "Bearer "+secret)
		} else {
			r.Header.Del("Authorization")
		}
		r.Host = target.Host
	}

	fileServer := http.FileServer(http.Dir(s.UIDir))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			proxy.ServeHTTP(w, r)
			return
		}
		// On the first authenticated visit to the root, send users into a
		// pre-filled zashboard setup so the panel connects to our same-origin
		// proxy with no manual entry. A one-shot cookie avoids a redirect loop.
		if r.URL.Path == "/" {
			if _, err := r.Cookie(setupCookie); err != nil {
				http.SetCookie(w, &http.Cookie{
					Name:     setupCookie,
					Value:    "1",
					Path:     "/",
					MaxAge:   int(setupCookieTTL.Seconds()),
					HttpOnly: true,
				})
				http.Redirect(w, r, s.setupURL(r), http.StatusFound)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The login endpoint is always reachable (it is what grants the cookie).
		if r.URL.Path == authLoginPath {
			s.handleLogin(w, r)
			return
		}
		if s.authed(r) {
			mux.ServeHTTP(w, r)
			return
		}
		// One-shot token in the query: validate, set the cookie, redirect clean.
		if tok := r.URL.Query().Get("token"); tok != "" && s.constantTimeEq(tok, secret) {
			s.setAuthCookie(w)
			q := r.URL.Query()
			q.Del("token")
			r.URL.RawQuery = q.Encode()
			http.Redirect(w, r, r.URL.String(), http.StatusFound)
			return
		}
		s.handleUnauthorized(w, r)
	})
}

// authed reports whether the request proves knowledge of the secret, via the
// session cookie (browsers) or a Bearer header (API clients).
func (s *Server) authed(r *http.Request) bool {
	if s.Secret == "" {
		return true // no secret configured -> no gate
	}
	if c, err := r.Cookie(authCookie); err == nil && s.constantTimeEq(c.Value, s.Secret) {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return s.constantTimeEq(strings.TrimPrefix(h, "Bearer "), s.Secret)
	}
	return false
}

// constantTimeEq is a constant-time string compare to avoid secret timing leaks.
func (s *Server) constantTimeEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// setAuthCookie grants a long-lived session cookie carrying the secret.
func (s *Server) setAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    s.Secret,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// handleLogin renders the login form (GET) or validates it (POST).
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderLogin(w, "")
		return
	}
	_ = r.ParseForm()
	if !s.constantTimeEq(r.PostForm.Get("password"), s.Secret) {
		w.WriteHeader(http.StatusUnauthorized)
		renderLogin(w, "incorrect secret")
		return
	}
	s.setAuthCookie(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleUnauthorized returns a 401 for API clients and the login page for browsers.
func (s *Server) handleUnauthorized(w http.ResponseWriter, r *http.Request) {
	if isAPIPath(r.URL.Path) || wantsJSON(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="zashhomo"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	renderLogin(w, "")
}

// wantsJSON reports whether the client prefers a JSON response.
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// isAPIPath reports whether path's first segment is a Clash API root.
func isAPIPath(path string) bool {
	seg := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
	return apiRoots[seg]
}

// setupURL builds the zashboard setup deep-link pointing at this origin. The
// panel talks to the same host:port it is served from, which we reverse-proxy.
// The secret is intentionally omitted: the web gate already proved the caller
// knows it, and the reverse proxy injects it into every proxied API request.
func (s *Server) setupURL(r *http.Request) string {
	host := r.Host // host:port as seen by the client
	hostname := host
	port := ""
	if i := strings.LastIndex(host, ":"); i >= 0 {
		hostname = host[:i]
		port = host[i+1:]
	}
	q := url.Values{}
	q.Set("hostname", hostname)
	if port != "" {
		q.Set("port", port)
	}
	return "/#/setup?" + q.Encode()
}

// renderLogin writes a minimal, dependency-free login form.
func renderLogin(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msgLine := ""
	if msg != "" {
		msgLine = `<p style="color:#ef4444;margin:6px 0 0">` + msg + `</p>`
	}
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>zashhomo</title>
<style>
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
  font:14px/1.5 system-ui,-apple-system,sans-serif;background:#0d1117;color:#e6edf3}
.card{width:340px;padding:28px 30px;background:#161b22;border:1px solid #30363d;border-radius:12px}
h1{margin:0;font-size:18px}
.muted{color:#7d8590;font-size:12px;margin:4px 0 18px}
input{width:100%%;padding:10px 12px;border:1px solid #30363d;border-radius:8px;background:#0d1117;color:#e6edf3;margin:4px 0}
button{width:100%%;padding:10px;border:0;border-radius:8px;background:#2563eb;color:#fff;font-size:14px;cursor:pointer;margin-top:8px}
</style>
</head>
<body>
<form class="card" method="post" action="%s">
<h1>zashhomo</h1>
<p class="muted">Enter the access secret to open the panel.</p>
%s
<input name="password" type="password" placeholder="secret" autofocus>
<button type="submit">Unlock</button>
</form>
</body>
</html>
`, authLoginPath, msgLine)
}

// Start begins serving. It returns once the listener is bound; serve errors are
// delivered on the returned channel.
func (s *Server) Start() <-chan error {
	s.httpServer = &http.Server{
		Addr:              s.Addr,
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		err := s.httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()
	return errCh
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}
