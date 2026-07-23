// Package web serves the zashboard static panel and reverse-proxies the Clash
// REST API (injecting the secret) so users open one address with no manual setup.
package web

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// setupCookie marks that a client has already been sent to the setup deep-link,
// so subsequent root requests serve the SPA instead of redirecting again.
const setupCookie = "zh_setup"

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
	// Addr is the listen address for the panel (e.g. 0.0.0.0:9091).
	Addr string
	// UIDir is the directory of zashboard static files.
	UIDir string
	// ControllerAddr is the kernel controller (e.g. 127.0.0.1:9090).
	ControllerAddr string
	// Secret is injected as a Bearer token on proxied API requests.
	Secret string

	httpServer *http.Server
}

// handler builds the mux: API paths -> reverse proxy, root -> setup redirect,
// everything else -> static files.
func (s *Server) handler() http.Handler {
	target := &url.URL{Scheme: "http", Host: s.ControllerAddr}
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	secret := s.Secret
	proxy.Director = func(r *http.Request) {
		baseDirector(r)
		// Overwrite any client-supplied credentials with the real secret so the
		// panel never needs to know it.
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
		// On the first visit to the root, send users into a pre-filled zashboard
		// setup so the panel connects to our same-origin proxy with no manual
		// entry. A one-shot cookie avoids a redirect loop: the target URL's hash
		// (#/setup?...) is not sent back to the server, so a plain redirect on "/"
		// would loop forever. After the cookie is set we serve the SPA normally.
		if r.URL.Path == "/" {
			if _, err := r.Cookie(setupCookie); err != nil {
				http.SetCookie(w, &http.Cookie{
					Name:     setupCookie,
					Value:    "1",
					Path:     "/",
					MaxAge:   int((365 * 24 * time.Hour).Seconds()),
					HttpOnly: true,
				})
				http.Redirect(w, r, s.setupURL(r), http.StatusFound)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
	return mux
}

// isAPIPath reports whether path's first segment is a Clash API root.
func isAPIPath(path string) bool {
	seg := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
	return apiRoots[seg]
}

// setupURL builds the zashboard setup deep-link pointing at this origin. The
// panel talks to the same host:port it is served from, which we reverse-proxy.
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
	// The proxy injects the secret, so the panel does not strictly require it;
	// passing it along lets zashboard connect even if pointed at the kernel
	// directly, and satisfies setup flows that expect the field.
	if s.Secret != "" {
		q.Set("secret", s.Secret)
	}
	return "/#/setup?" + q.Encode()
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
