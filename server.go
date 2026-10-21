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
	"strconv"
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
	cfg  *Config
	auth *Auth
	sp   *Spotify
	lib  *Library
	mux  *http.ServeMux
}

func NewServer(cfg *Config, auth *Auth, sp *Spotify) *Server {
	s := &Server{cfg: cfg, auth: auth, sp: sp, lib: NewLibrary(sp), mux: http.NewServeMux()}
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

type session struct {
	Configured  bool   `json:"configured"`
	LoggedIn    bool   `json:"loggedIn"`
	User        *User  `json:"user,omitempty"`
	RedirectURI string `json:"redirectUri"`
	Error       string `json:"error,omitempty"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	out := session{Configured: s.cfg.clientID() != "", RedirectURI: s.auth.redirectURI}
	if out.Configured && s.cfg.token() != nil {
		var u User
		err := s.sp.get(r.Context(), "/me", nil, &u)
		switch {
		case err == nil:
			out.LoggedIn, out.User = true, &u
		case errors.Is(err, errNotLoggedIn):
		default:
			out.LoggedIn = s.cfg.token() != nil
			out.Error = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, out)
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

// TrackInfo is the now-playing track as the UI shows it.
type TrackInfo struct {
	Name       string `json:"name"`
	URI        string `json:"uri"`
	Artists    string `json:"artists"`
	Album      string `json:"album"`
	DurationMs int    `json:"durationMs"`
}

type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	IsActive bool   `json:"is_active"`
	Volume   *int   `json:"volume_percent"`
}

// PlayerState is the playback state of whichever device is active, for when it isn't this page.
type PlayerState struct {
	Device     Device     `json:"device"`
	Playing    bool       `json:"playing"`
	PositionMs int        `json:"positionMs"`
	Shuffle    bool       `json:"shuffle"`
	Repeat     string     `json:"repeat"`
	Track      *TrackInfo `json:"track"`
}

func (s *Server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Device     *Device `json:"device"`
		IsPlaying  bool    `json:"is_playing"`
		ProgressMs int     `json:"progress_ms"`
		Shuffle    bool    `json:"shuffle_state"`
		Repeat     string  `json:"repeat_state"`
		Item       *Track  `json:"item"`
	}
	if err := s.sp.get(r.Context(), "/me/player", url.Values{"additional_types": {"episode"}}, &raw); err != nil {
		apiFail(w, err)
		return
	}
	if raw.Device == nil { // 204: nothing playing anywhere.
		writeJSON(w, http.StatusOK, nil)
		return
	}
	st := PlayerState{Device: *raw.Device, Playing: raw.IsPlaying, PositionMs: raw.ProgressMs, Shuffle: raw.Shuffle, Repeat: raw.Repeat}
	if t := raw.Item; t != nil {
		st.Track = &TrackInfo{Name: t.Name, URI: t.URI, Artists: artistNames(t.Artists), DurationMs: t.DurationMs}
		if t.Album != nil {
			st.Track.Album = t.Album.Name
		} else if t.Show != nil {
			st.Track.Album = t.Show.Name
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	var res struct {
		Devices []Device `json:"devices"`
	}
	if err := s.sp.get(r.Context(), "/me/player/devices", nil, &res); err != nil {
		apiFail(w, err)
		return
	}
	if res.Devices == nil {
		res.Devices = []Device{}
	}
	writeJSON(w, http.StatusOK, res.Devices)
}

// playerAction is the body of POST /api/player/{action}.
type playerAction struct {
	DeviceID   string   `json:"deviceId"`
	Context    string   `json:"context"`
	URIs       []string `json:"uris"`
	OffsetURI  string   `json:"offsetUri"`
	PositionMs int      `json:"positionMs"`
	Volume     int      `json:"volume"`
	Shuffle    bool     `json:"shuffle"`
	Repeat     string   `json:"repeat"`
	Play       bool     `json:"play"`
}

const maxQueueURIs = 500

func (s *Server) handlePlayerAction(w http.ResponseWriter, r *http.Request) {
	var a playerAction
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeError(w, http.StatusBadRequest, "Bad request body")
			return
		}
	}
	q := url.Values{}
	if a.DeviceID != "" {
		q.Set("device_id", a.DeviceID)
	}
	var (
		method = http.MethodPut
		p      string
		body   any
	)
	switch r.PathValue("action") {
	case "play": // Start a context or a list of tracks.
		b := map[string]any{}
		switch {
		case a.Context != "":
			b["context_uri"] = a.Context
			if a.OffsetURI != "" {
				b["offset"] = map[string]string{"uri": a.OffsetURI}
			}
		case len(a.URIs) > 0:
			uris := a.URIs
			if len(uris) > maxQueueURIs { // Keep the request small: start from the chosen track.
				i := max(0, indexOf(uris, a.OffsetURI))
				uris = uris[i:min(len(uris), i+maxQueueURIs)]
			}
			b["uris"] = uris
			if a.OffsetURI != "" {
				b["offset"] = map[string]string{"uri": a.OffsetURI}
			}
		default:
			writeError(w, http.StatusBadRequest, "Nothing to play")
			return
		}
		p, body = "/me/player/play", b
	case "resume":
		p = "/me/player/play"
	case "pause":
		p = "/me/player/pause"
	case "next":
		method, p = http.MethodPost, "/me/player/next"
	case "previous":
		method, p = http.MethodPost, "/me/player/previous"
	case "seek":
		p = "/me/player/seek"
		q.Set("position_ms", strconv.Itoa(max(0, a.PositionMs)))
	case "volume":
		p = "/me/player/volume"
		q.Set("volume_percent", strconv.Itoa(min(100, max(0, a.Volume))))
	case "shuffle":
		p = "/me/player/shuffle"
		q.Set("state", strconv.FormatBool(a.Shuffle))
	case "repeat":
		if a.Repeat != "off" && a.Repeat != "context" && a.Repeat != "track" {
			writeError(w, http.StatusBadRequest, "repeat must be off, context or track")
			return
		}
		p = "/me/player/repeat"
		q.Set("state", a.Repeat)
	case "transfer":
		if a.DeviceID == "" {
			writeError(w, http.StatusBadRequest, "Missing deviceId")
			return
		}
		p, q = "/me/player", nil
		body = map[string]any{"device_ids": []string{a.DeviceID}, "play": a.Play}
	default:
		writeError(w, http.StatusNotFound, "Unknown action")
		return
	}
	if err := s.sp.do(r.Context(), method, p, q, body, nil); err != nil {
		apiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
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
