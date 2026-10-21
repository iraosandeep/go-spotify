package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Links people paste: https://open.spotify.com/(intl-xx/)(embed/)playlist/<id>?si=…, spotify:album:<id>,
// and the short links the Spotify apps share (spotify.link/…, spoti.fi/…).
var (
	linkRe  = regexp.MustCompile(`(?:open|play)\.spotify\.com/(?:intl-[a-zA-Z-]+/)?(?:embed/)?(?:user/[^/]+/)?(track|album|playlist|artist|episode|show)/([A-Za-z0-9]{22})`)
	uriRe   = regexp.MustCompile(`^spotify:(?:user:[^:]+:)?(track|album|playlist|artist|episode|show):([A-Za-z0-9]{22})$`)
	idRe    = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)
	shortRe = regexp.MustCompile(`^(?:https?://)?(spotify\.link|spoti\.fi)/[A-Za-z0-9_-]+`)

	errNotSpotifyLink = errors.New("not a Spotify link: paste an open.spotify.com URL or a spotify: URI")
)

func validID(id string) bool { return idRe.MatchString(id) }

// parseLink extracts the kind and ID from an open.spotify.com URL or spotify: URI.
func parseLink(s string) (kind, id string, ok bool) {
	s = strings.TrimSpace(s)
	if m := uriRe.FindStringSubmatch(s); m != nil {
		return m[1], m[2], true
	}
	if m := linkRe.FindStringSubmatch(s); m != nil {
		return m[1], m[2], true
	}
	return "", "", false
}

// Resolved says what a link points at and how to open it.
type Resolved struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
	// Path is the library folder to show for it, if there is one.
	Path string      `json:"path,omitempty"`
	Play PlayRequest `json:"play"`
}

type PlayRequest struct {
	Context   string   `json:"context,omitempty"`
	URIs      []string `json:"uris,omitempty"`
	OffsetURI string   `json:"offsetUri,omitempty"`
}

// expandShortLink follows a spotify.link / spoti.fi redirect to the open.spotify.com URL behind it.
func expandShortLink(ctx context.Context, link string) (string, error) {
	if !strings.HasPrefix(link, "http") {
		link = "https://" + link
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	var final string
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if linkRe.MatchString(r.URL.String()) {
				final = r.URL.String()
				return http.ErrUseLastResponse // Got it; no need to load the web player.
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if final != "" {
		return final, nil
	}
	// Some short links answer with an HTML page that carries the real URL.
	b, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if m := linkRe.FindString(string(b)); m != "" {
		return "https://" + m, nil
	}
	return "", errNotSpotifyLink
}

// Resolve turns a pasted link into something to show and play. Names are best effort: Spotify
// hides some objects (e.g. its editorial playlists) from apps, but they still play.
func (s *Server) Resolve(ctx context.Context, link string) (*Resolved, error) {
	link = strings.TrimSpace(link)
	if shortRe.MatchString(link) {
		full, err := expandShortLink(ctx, link)
		if err != nil {
			return nil, err
		}
		link = full
	}
	kind, id, ok := parseLink(link)
	if !ok {
		return nil, errNotSpotifyLink
	}
	r := &Resolved{Kind: kind, ID: id, URI: "spotify:" + kind + ":" + id}
	var meta struct {
		Name  string `json:"name"`
		Album *Album `json:"album"`
	}
	q := url.Values{}
	if kind == "playlist" {
		q.Set("fields", "name")
	}
	if err := s.sp.get(ctx, "/"+kind+"s/"+id, q, &meta); err != nil {
		if st := statusOf(err); st != 403 && st != 404 {
			return nil, err
		}
	}
	r.Name = meta.Name

	switch kind {
	case "playlist":
		r.Path, r.Play = "playlists/"+id, PlayRequest{Context: r.URI}
	case "album":
		r.Path, r.Play = "albums/"+id, PlayRequest{Context: r.URI}
	case "artist":
		r.Path, r.Play = "artists/"+id, PlayRequest{Context: r.URI}
	case "show":
		r.Play = PlayRequest{Context: r.URI}
	case "track":
		// Show the track in its album and play on through the album from there.
		if meta.Album != nil && validID(meta.Album.ID) {
			r.Path = "albums/" + meta.Album.ID
			r.Play = PlayRequest{Context: meta.Album.URI, OffsetURI: r.URI}
		} else {
			r.Play = PlayRequest{URIs: []string{r.URI}}
		}
	default: // episode
		r.Play = PlayRequest{URIs: []string{r.URI}}
	}
	return r, nil
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	res, err := s.Resolve(r.Context(), r.URL.Query().Get("url"))
	if errors.Is(err, errNotSpotifyLink) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		apiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
