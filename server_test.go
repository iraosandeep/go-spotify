package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSpotify stands in for both accounts.spotify.com and api.spotify.com.
type fakeSpotify struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string
}

func (f *fakeSpotify) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	f.bodies[r.URL.Path] = string(b)
	f.mu.Unlock()

	if r.URL.Path == "/api/token" {
		fmt.Fprint(w, `{"access_token":"fresh","refresh_token":"rt2","expires_in":3600}`)
		return
	}
	if r.Header.Get("Authorization") != "Bearer fresh" && r.Header.Get("Authorization") != "Bearer valid" {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"status":401,"message":"Invalid access token"}}`)
		return
	}
	switch r.URL.Path {
	case "/v1/me":
		fmt.Fprint(w, `{"id":"u1","display_name":"Sandy"}`)
	case "/v1/me/playlists":
		fmt.Fprint(w, `{"total":2,"items":[
			{"id":"aaaaaaaaaaaaaaaaaaaaa1","name":"Mine","uri":"spotify:playlist:aaaaaaaaaaaaaaaaaaaaa1","owner":{"display_name":"Sandy"},"items":{"total":2}},
			{"id":"bbbbbbbbbbbbbbbbbbbbb2","name":"Theirs","uri":"spotify:playlist:bbbbbbbbbbbbbbbbbbbbb2","owner":{"display_name":"Spotify"},"tracks":{"total":50}}]}`)
	case "/v1/playlists/aaaaaaaaaaaaaaaaaaaaa1":
		fmt.Fprint(w, `{"id":"aaaaaaaaaaaaaaaaaaaaa1","name":"Mine","uri":"spotify:playlist:aaaaaaaaaaaaaaaaaaaaa1"}`)
	case "/v1/playlists/aaaaaaaaaaaaaaaaaaaaa1/items":
		fmt.Fprint(w, `{"total":3,"items":[
			{"item":{"name":"A","uri":"spotify:track:a","duration_ms":1000,"artists":[{"name":"X"},{"name":"Y"}],"album":{"name":"Al"}}},
			{"item":null},
			{"track":{"name":"B","uri":"spotify:track:b","duration_ms":2000,"artists":[{"name":"Z"}]}}]}`)
	case "/v1/playlists/bbbbbbbbbbbbbbbbbbbbb2":
		fmt.Fprint(w, `{"id":"bbbbbbbbbbbbbbbbbbbbb2","name":"Theirs","uri":"spotify:playlist:bbbbbbbbbbbbbbbbbbbbb2"}`)
	case "/v1/playlists/bbbbbbbbbbbbbbbbbbbbb2/items":
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":{"status":403,"message":"Forbidden"}}`)
	case "/v1/me/tracks":
		// 120 liked songs, paged 50 at a time.
		off := 0
		fmt.Sscan(r.URL.Query().Get("offset"), &off)
		items := []string{}
		for i := off; i < min(off+50, 120); i++ {
			items = append(items, fmt.Sprintf(`{"track":{"name":"T%d","uri":"spotify:track:%d","artists":[]}}`, i, i))
		}
		fmt.Fprintf(w, `{"total":120,"items":[%s]}`, strings.Join(items, ","))
	case "/v1/tracks/4uLU6hMCjMI75M1A2tKUQC":
		fmt.Fprint(w, `{"name":"Never Gonna Give You Up","album":{"id":"6XzmaSxBWtl0Up3QuZgVmH","uri":"spotify:album:6XzmaSxBWtl0Up3QuZgVmH","name":"Whenever"}}`)
	case "/v1/me/player/play":
		w.WriteHeader(204)
	case "/v1/me/player":
		w.WriteHeader(204)
	default:
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"status":404,"message":"no fake for this"}}`)
	}
}

func newTestServer(t *testing.T, tok *Token) (*Server, *fakeSpotify) {
	t.Helper()
	fake := &fakeSpotify{bodies: map[string]string{}}
	ts := httptest.NewServer(fake)
	t.Cleanup(ts.Close)
	cfg := loadConfig(filepath.Join(t.TempDir(), "config.json"))
	cfg.ClientID = "cid"
	cfg.Token = tok
	auth := NewAuth(cfg, "http://127.0.0.1:7172/callback")
	auth.accountsURL = ts.URL
	sp := NewSpotify(auth)
	sp.base = ts.URL + "/v1"
	return NewServer(cfg, auth, sp), fake
}

func validToken() *Token {
	return &Token{AccessToken: "valid", RefreshToken: "rt", Expiry: time.Now().Add(time.Hour)}
}

func request(s *Server, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:7172"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("bad JSON %q: %v", w.Body.String(), err)
	}
	return v
}

func TestRootListsSections(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	w := request(s, "GET", "/api/files?path=", "")
	l := decode[Listing](t, w)
	if w.Code != 200 || len(l.Entries) != len(sections) || l.Entries[0].Path != "liked" {
		t.Fatalf("got %d %+v", w.Code, l)
	}
}

func TestPlaylistsAndItems(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	l := decode[Listing](t, request(s, "GET", "/api/files?path=playlists", ""))
	if len(l.Entries) != 2 || l.Entries[0].Detail != "2 tracks · Sandy" || l.Entries[1].Detail != "50 tracks · Spotify" {
		t.Fatalf("playlists: %+v", l.Entries)
	}

	l = decode[Listing](t, request(s, "GET", "/api/files?path=playlists/aaaaaaaaaaaaaaaaaaaaa1", ""))
	if l.Context != "spotify:playlist:aaaaaaaaaaaaaaaaaaaaa1" || strings.Join(l.Crumbs, "/") != "Playlists/Mine" {
		t.Fatalf("playlist: %+v", l)
	}
	// "item" (2026 API) and "track" (older) both parse; removed tracks are dropped.
	if len(l.Entries) != 2 || l.Entries[0].Detail != "X, Y" || l.Entries[1].Name != "B" {
		t.Fatalf("items: %+v", l.Entries)
	}
}

func TestPlaylistNotOwnedFallsBackToContext(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	w := request(s, "GET", "/api/files?path=playlists/bbbbbbbbbbbbbbbbbbbbb2", "")
	l := decode[Listing](t, w)
	if w.Code != 200 || len(l.Entries) != 1 || l.Entries[0].Type != "context" || l.Notice == "" {
		t.Fatalf("got %d %+v", w.Code, l)
	}
}

func TestLikedPagesInOrder(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	l := decode[Listing](t, request(s, "GET", "/api/files?path=liked", ""))
	if len(l.Entries) != 120 {
		t.Fatalf("got %d entries", len(l.Entries))
	}
	for i, e := range l.Entries {
		if e.Name != fmt.Sprintf("T%d", i) {
			t.Fatalf("entry %d is %s", i, e.Name)
		}
	}
}

func TestUnknownFolder(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	if w := request(s, "GET", "/api/files?path=nope", ""); w.Code != 404 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestNotLoggedIn(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if w := request(s, "GET", "/api/files?path=liked", ""); w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
	sess := decode[session](t, request(s, "GET", "/api/session", ""))
	if !sess.Configured || sess.LoggedIn {
		t.Fatalf("session %+v", sess)
	}
}

func TestExpiredTokenIsRefreshed(t *testing.T) {
	s, fake := newTestServer(t, &Token{AccessToken: "old", RefreshToken: "rt", Expiry: time.Now().Add(-time.Minute)})
	sess := decode[session](t, request(s, "GET", "/api/session", ""))
	if !sess.LoggedIn || sess.User.DisplayName != "Sandy" {
		t.Fatalf("session %+v", sess)
	}
	if !strings.Contains(fake.bodies["/api/token"], "grant_type=refresh_token") {
		t.Fatalf("no refresh: %q", fake.bodies["/api/token"])
	}
	if tok := s.cfg.token(); tok.AccessToken != "fresh" || tok.RefreshToken != "rt2" {
		t.Fatalf("token not saved: %+v", tok)
	}
}

func TestPlayContextWithOffset(t *testing.T) {
	s, fake := newTestServer(t, validToken())
	w := request(s, "POST", "/api/player/play", `{"deviceId":"dev1","context":"spotify:album:x","offsetUri":"spotify:track:b"}`)
	if w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if last := fake.requests[len(fake.requests)-1]; last != "PUT /v1/me/player/play?device_id=dev1" {
		t.Fatalf("request %s", last)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(fake.bodies["/v1/me/player/play"]), &body)
	if body["context_uri"] != "spotify:album:x" || body["offset"].(map[string]any)["uri"] != "spotify:track:b" {
		t.Fatalf("body %v", body)
	}
}

func TestPlayLongQueueStartsAtChosenTrack(t *testing.T) {
	s, fake := newTestServer(t, validToken())
	uris := make([]string, 900)
	for i := range uris {
		uris[i] = fmt.Sprintf("spotify:track:%d", i)
	}
	b, _ := json.Marshal(map[string]any{"uris": uris, "offsetUri": "spotify:track:700"})
	if w := request(s, "POST", "/api/player/play", string(b)); w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
	var body struct{ URIs []string }
	_ = json.Unmarshal([]byte(fake.bodies["/v1/me/player/play"]), &body)
	if len(body.URIs) != 200 || body.URIs[0] != "spotify:track:700" {
		t.Fatalf("sent %d uris starting %v", len(body.URIs), body.URIs[:1])
	}
}

func TestPlayerIdle(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	w := request(s, "GET", "/api/player", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatalf("got %d %q", w.Code, w.Body)
	}
}

func TestRejectsForeignHostAndOrigin(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	req := httptest.NewRequest("GET", "/api/token", nil)
	req.Host = "evil.example:7172"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("foreign host: %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/logout", nil)
	req.Host = "127.0.0.1:7172"
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 403 || s.cfg.token() == nil {
		t.Fatalf("foreign origin: %d", w.Code)
	}
}

func TestLoginURLUsesPKCE(t *testing.T) {
	s, _ := newTestServer(t, nil)
	w := request(s, "GET", "/login", "")
	loc := w.Header().Get("Location")
	for _, want := range []string{"code_challenge_method=S256", "client_id=cid", "streaming", "redirect_uri=http%3A%2F%2F127.0.0.1%3A7172%2Fcallback"} {
		if !strings.Contains(loc, want) {
			t.Fatalf("login URL %s missing %s", loc, want)
		}
	}
}

func TestParseLink(t *testing.T) {
	const id = "37i9dQZF1DX5trt9i14X7j"
	for _, in := range []string{
		"https://open.spotify.com/playlist/" + id,
		"https://open.spotify.com/playlist/" + id + "?si=abc123&pi=x",
		"  open.spotify.com/intl-de/playlist/" + id + "  ",
		"https://open.spotify.com/embed/playlist/" + id,
		"https://open.spotify.com/user/spotify/playlist/" + id,
		"spotify:playlist:" + id,
		"spotify:user:spotify:playlist:" + id,
	} {
		kind, got, ok := parseLink(in)
		if !ok || kind != "playlist" || got != id {
			t.Errorf("%q: got %s %s %v", in, kind, got, ok)
		}
	}
	for _, in := range []string{"https://example.com/playlist/" + id, "spotify:playlist:short", "hello"} {
		if _, _, ok := parseLink(in); ok {
			t.Errorf("%q parsed", in)
		}
	}
}

func TestResolveHiddenEditorialPlaylist(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	link := "https://open.spotify.com/playlist/37i9dQZF1DX5trt9i14X7j?si=x"
	w := request(s, "GET", "/api/resolve?url="+url.QueryEscape(link), "")
	r := decode[Resolved](t, w)
	if w.Code != 200 || r.Path != "playlists/37i9dQZF1DX5trt9i14X7j" || r.Play.Context != "spotify:playlist:37i9dQZF1DX5trt9i14X7j" {
		t.Fatalf("got %d %+v", w.Code, r)
	}
	// Its folder still opens, with a row that plays it.
	l := decode[Listing](t, request(s, "GET", "/api/files?path="+r.Path, ""))
	if len(l.Entries) != 1 || l.Entries[0].Type != "context" || l.Context != r.Play.Context {
		t.Fatalf("listing %+v", l)
	}
}

func TestResolveTrackOpensItsAlbum(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	r := decode[Resolved](t, request(s, "GET", "/api/resolve?url="+url.QueryEscape("https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC"), ""))
	if r.Name != "Never Gonna Give You Up" || r.Path != "albums/6XzmaSxBWtl0Up3QuZgVmH" ||
		r.Play.Context != "spotify:album:6XzmaSxBWtl0Up3QuZgVmH" || r.Play.OffsetURI != "spotify:track:4uLU6hMCjMI75M1A2tKUQC" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveRejectsOtherLinks(t *testing.T) {
	s, _ := newTestServer(t, validToken())
	if w := request(s, "GET", "/api/resolve?url=https://example.com", ""); w.Code != 400 {
		t.Fatalf("got %d", w.Code)
	}
}
