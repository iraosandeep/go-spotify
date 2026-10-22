package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed all:web/dist
var embedded embed.FS

// assets is the embedded web/dist directory, or the one on disk under -dev.
var assets fs.FS = mustSub(embedded, "web/dist")

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// useDiskAssets serves the UI from a built directory on disk so it can be rebuilt without
// recompiling the binary. Development convenience only.
func useDiskAssets(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return err
	}
	assets = os.DirFS(dir)
	return nil
}

type Server struct {
	cfg    *Config
	auth   *Auth
	sp     *Spotify
	lib    *Library
	player *Player
	mux    *http.ServeMux
}

func NewServer(cfg *Config, auth *Auth, sp *Spotify) *Server {
	s := &Server{cfg: cfg, auth: auth, sp: sp, lib: NewLibrary(sp), player: NewPlayer(sp), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /login", s.handleLogin)
	s.mux.HandleFunc("GET /callback", s.handleCallback)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("POST /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/token", s.handleToken)
	s.mux.HandleFunc("GET /api/files", s.handleFiles)
	s.mux.HandleFunc("GET /api/resolve", s.handleResolve)
	s.mux.HandleFunc("GET /api/player", s.handlePlayer)
	s.mux.HandleFunc("GET /api/devices", s.handleDevices)
	s.mux.HandleFunc("POST /api/player/{action}", s.handlePlayerAction)
	s.mux.HandleFunc("GET /", s.handleStatic)
	return s
}

// ServeHTTP only answers requests addressed to this machine (no DNS rebinding onto the token
// endpoint) and refuses requests that other websites send from the user's browser.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || u.Host != r.Host {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// apiFail turns an error from the Spotify client into a response.
func apiFail(w http.ResponseWriter, err error) {
	var ae *APIError
	var nf notFoundError
	switch {
	case errors.Is(err, errNotLoggedIn), errors.Is(err, errNoClientID):
		writeError(w, http.StatusUnauthorized, "Log in with Spotify first")
	case errors.As(err, &nf):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &ae):
		status := ae.Status
		if status == http.StatusUnauthorized {
			status = http.StatusBadGateway // Our session is fine; Spotify refused the token.
		}
		writeError(w, status, ae.Message)
	case errors.Is(err, context.Canceled):
		writeError(w, 499, "cancelled")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	u, err := s.auth.LoginURL()
	if err != nil {
		http.Redirect(w, r, "/?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		http.Redirect(w, r, "/?error="+url.QueryEscape("Spotify login: "+e), http.StatusFound)
		return
	}
	if err := s.auth.Exchange(r.Context(), q.Get("state"), q.Get("code")); err != nil {
		http.Redirect(w, r, "/?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	s.lib.Forget()
	uiStatus("ok", "logged in to Spotify", "", os.Stdout)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Logout(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.lib.Forget()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, checkSession(r.Context(), s.cfg, s.sp, s.auth.redirectURI))
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID string `json:"clientId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ClientID) == "" {
		writeError(w, http.StatusBadRequest, "Missing client ID")
		return
	}
	if err := s.cfg.setClientID(strings.TrimSpace(body.ClientID)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.lib.Forget()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleToken hands the access token to the Web Playback SDK running in the page.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	tok, err := s.auth.AccessToken(r.Context())
	if err != nil {
		apiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"accessToken": tok})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	l, err := s.lib.List(r.Context(), q.Get("path"), q.Get("q"), q.Get("fresh") == "1")
	if err != nil {
		apiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	st, err := s.player.State(r.Context())
	if err != nil {
		apiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	devs, err := s.player.Devices(r.Context())
	if err != nil {
		apiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, devs)
}

func (s *Server) handlePlayerAction(w http.ResponseWriter, r *http.Request) {
	var a playerAction
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeError(w, http.StatusBadRequest, "Bad request body")
			return
		}
	}
	if err := s.player.Do(r.Context(), r.PathValue("action"), a); err != nil {
		switch {
		case errors.Is(err, errNothingToPlay):
			writeError(w, http.StatusBadRequest, "Nothing to play")
		case errors.Is(err, errMissingDeviceID):
			writeError(w, http.StatusBadRequest, "Missing deviceId")
		case errors.Is(err, errBadRepeatValue):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, errUnknownPlayerAction):
			writeError(w, http.StatusNotFound, "Unknown action")
		default:
			apiFail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleStatic serves the embedded UI; unknown paths fall back to index.html.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "index.html" {
		serveIndex(w, r)
		return
	}
	if st, err := fs.Stat(assets, name); err != nil || st.IsDir() {
		serveIndex(w, r)
		return
	}
	// Bundled assets have content hashes in their names, so they never change.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFileFS(w, r, assets, name)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "UI not built: run `make web`", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}
