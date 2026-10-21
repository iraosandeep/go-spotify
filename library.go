package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The library is presented as a folder tree, like the vstream file browser:
//
//	Liked Songs/  Playlists/<id>  Albums/<id>  Artists/<id>/<album id>  Recently Played/  Top Tracks/  Search/
//
// Paths are slash-separated keys ("playlists/37i9…"); names come back in Crumbs for the title.

// Entry is one row of a listing.
type Entry struct {
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	Type       string `json:"type"` // "dir", "track", or "context" (play the whole listing's context)
	URI        string `json:"uri,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Album      string `json:"album,omitempty"`
	DurationMs int    `json:"durationMs,omitempty"`
}

// Listing is one folder of the library.
type Listing struct {
	Path   string   `json:"path"`
	Crumbs []string `json:"crumbs"`
	// Context is the Spotify URI (album/playlist) the tracks play within, so next/prev,
	// shuffle and repeat are handled by Spotify itself. Empty for plain track lists.
	Context string  `json:"context,omitempty"`
	Notice  string  `json:"notice,omitempty"`
	Entries []Entry `json:"entries"`
}

const (
	maxTracks   = 2000
	maxItems    = 500
	searchPages = 5 // Development Mode apps get 10 search results per request.
	cacheTTL    = 90 * time.Second
)

var sections = []struct{ key, name string }{
	{"liked", "Liked Songs"},
	{"playlists", "Playlists"},
	{"albums", "Albums"},
	{"artists", "Artists"},
	{"recent", "Recently Played"},
	{"top", "Top Tracks"},
	{"search", "Search"},
}

// Library builds listings and caches them briefly so going back up the tree is instant.
type Library struct {
	sp    *Spotify
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at time.Time
	l  *Listing
}

func NewLibrary(sp *Spotify) *Library {
	return &Library{sp: sp, cache: map[string]cached{}}
}

func (lib *Library) Forget() {
	lib.mu.Lock()
	lib.cache = map[string]cached{}
	lib.mu.Unlock()
}

// List returns the listing at path; q is the search query for "search".
func (lib *Library) List(ctx context.Context, path, q string, fresh bool) (*Listing, error) {
	path = strings.Trim(path, "/")
	key := path + "\x00" + q
	lib.mu.Lock()
	c, ok := lib.cache[key]
	lib.mu.Unlock()
	if ok && !fresh && time.Since(c.at) < cacheTTL {
		return c.l, nil
	}
	l, err := lib.list(ctx, path, q)
	if err != nil {
		return nil, err
	}
	l.Path = path
	if l.Entries == nil {
		l.Entries = []Entry{}
	}
	lib.mu.Lock()
	lib.cache[key] = cached{time.Now(), l}
	lib.mu.Unlock()
	return l, nil
}

type notFoundError string

func (e notFoundError) Error() string { return string(e) }

func (lib *Library) list(ctx context.Context, path, q string) (*Listing, error) {
	parts := strings.Split(path, "/")
	if path == "" {
		l := &Listing{Crumbs: []string{}}
		for _, s := range sections {
			l.Entries = append(l.Entries, Entry{Name: s.name, Path: s.key, Type: "dir"})
		}
		return l, nil
	}
	switch {
	case path == "liked":
		return lib.liked(ctx)
	case path == "recent":
		return lib.recent(ctx)
	case path == "top":
		return lib.top(ctx)
	case path == "search":
		return lib.search(ctx, q)
	case path == "playlists":
		return lib.playlists(ctx)
	case parts[0] == "playlists" && len(parts) == 2:
		return lib.playlist(ctx, parts[1])
	case path == "albums":
		return lib.albums(ctx)
	case parts[0] == "albums" && len(parts) == 2:
		return lib.album(ctx, parts[1], []string{"Albums"})
	case path == "artists":
		return lib.artists(ctx)
	case parts[0] == "artists" && len(parts) == 2:
		return lib.artist(ctx, parts[1])
	case parts[0] == "artists" && len(parts) == 3:
		var a Artist
		if err := lib.sp.get(ctx, "/artists/"+url.PathEscape(parts[1]), nil, &a); err != nil {
			return nil, err
		}
		return lib.album(ctx, parts[2], []string{"Artists", a.Name})
	}
	return nil, notFoundError("no such folder: " + path)
}

func trackEntry(t *Track) Entry {
	e := Entry{Name: t.Name, Type: "track", URI: t.URI, DurationMs: t.DurationMs, Detail: artistNames(t.Artists)}
	if t.Album != nil {
		e.Album = t.Album.Name
	}
	if t.Show != nil && e.Detail == "" {
		e.Detail = t.Show.Name
	}
	return e
}

// playable drops removed tracks (null) and local files, which the Web API can't start.
func playable(t *Track) bool { return t != nil && t.URI != "" && !t.IsLocal }

func (lib *Library) liked(ctx context.Context) (*Listing, error) {
	items, _, err := fetchAll[struct{ Track *Track }](ctx, lib.sp, "/me/tracks", nil, 50, maxTracks)
	if err != nil {
		return nil, err
	}
	l := &Listing{Crumbs: []string{"Liked Songs"}}
	for _, it := range items {
		if playable(it.Track) {
			l.Entries = append(l.Entries, trackEntry(it.Track))
		}
	}
	return l, nil
}

func (lib *Library) recent(ctx context.Context) (*Listing, error) {
	var page struct {
		Items []struct{ Track *Track } `json:"items"`
	}
	if err := lib.sp.get(ctx, "/me/player/recently-played", url.Values{"limit": {"50"}}, &page); err != nil {
		return nil, err
	}
	l := &Listing{Crumbs: []string{"Recently Played"}}
	for _, it := range page.Items {
		if playable(it.Track) {
			l.Entries = append(l.Entries, trackEntry(it.Track))
		}
	}
	return l, nil
}

func (lib *Library) top(ctx context.Context) (*Listing, error) {
	tracks, _, err := fetchAll[*Track](ctx, lib.sp, "/me/top/tracks", url.Values{"time_range": {"medium_term"}}, 50, 100)
	if err != nil {
		return nil, err
	}
	l := &Listing{Crumbs: []string{"Top Tracks"}}
	for _, t := range tracks {
		if playable(t) {
			l.Entries = append(l.Entries, trackEntry(t))
		}
	}
	return l, nil
}

func (lib *Library) search(ctx context.Context, q string) (*Listing, error) {
	q = strings.TrimSpace(q)
	l := &Listing{Crumbs: []string{"Search"}}
	if q == "" {
		l.Notice = "Press / and type to search Spotify, Enter to run it"
		return l, nil
	}
	l.Crumbs = append(l.Crumbs, fmt.Sprintf("%q", q))
	pages := make([][]*Track, searchPages)
	errs := make([]error, searchPages)
	var wg sync.WaitGroup
	for i := range searchPages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res struct {
				Tracks Page[*Track] `json:"tracks"`
			}
			errs[i] = lib.sp.get(ctx, "/search", url.Values{
				"q": {q}, "type": {"track"}, "limit": {"10"}, "offset": {strconv.Itoa(i * 10)},
			}, &res)
			pages[i] = res.Tracks.Items
		}()
	}
	wg.Wait()
	if errs[0] != nil {
		return nil, errs[0]
	}
	seen := map[string]bool{}
	for _, p := range pages {
		for _, t := range p {
			if playable(t) && !seen[t.URI] {
				seen[t.URI] = true
				l.Entries = append(l.Entries, trackEntry(t))
			}
		}
	}
	return l, nil
}

func (lib *Library) playlists(ctx context.Context) (*Listing, error) {
	pls, _, err := fetchAll[*Playlist](ctx, lib.sp, "/me/playlists", nil, 50, maxItems)
	if err != nil {
		return nil, err
	}
	l := &Listing{Crumbs: []string{"Playlists"}}
	for _, p := range pls {
		if p == nil {
			continue
		}
		detail := p.Owner.DisplayName
		if n := p.total(); n >= 0 {
			detail = fmt.Sprintf("%d tracks · %s", n, detail)
		}
		l.Entries = append(l.Entries, Entry{Name: p.Name, Path: "playlists/" + p.ID, Type: "dir", URI: p.URI, Detail: detail})
	}
	return l, nil
}

func (lib *Library) playlist(ctx context.Context, id string) (*Listing, error) {
	if !validID(id) {
		return nil, notFoundError("no such playlist: " + id)
	}
	var p Playlist
	err := lib.sp.get(ctx, "/playlists/"+id, url.Values{"fields": {"id,name,uri"}}, &p)
	if s := statusOf(err); s == 403 || s == 404 {
		// Spotify hides its own editorial and algorithmic playlists (37i9…) from apps, but
		// they still play as a context.
		uri := "spotify:playlist:" + id
		return &Listing{
			Crumbs:  []string{"Playlists", id},
			Context: uri,
			Notice:  "Spotify doesn't share this playlist's tracks with apps; Enter plays it",
			Entries: []Entry{{Name: "Play playlist", Type: "context", URI: uri}},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	l := &Listing{Crumbs: []string{"Playlists", p.Name}, Context: p.URI}
	items, _, err := fetchAll[PlaylistItem](ctx, lib.sp, "/playlists/"+id+"/items", nil, 50, maxTracks)
	if s := statusOf(err); s == 403 || s == 404 {
		// Spotify only lists tracks of playlists the user owns or collaborates on, but any
		// playlist can still be played as a whole.
		l.Notice = "Spotify only lists tracks of playlists you own; Enter plays it"
		l.Entries = []Entry{{Name: "Play " + p.Name, Type: "context", URI: p.URI}}
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if t := it.track(); playable(t) {
			l.Entries = append(l.Entries, trackEntry(t))
		}
	}
	return l, nil
}

func (lib *Library) albums(ctx context.Context) (*Listing, error) {
	items, _, err := fetchAll[struct{ Album *Album }](ctx, lib.sp, "/me/albums", nil, 50, maxItems)
	if err != nil {
		return nil, err
	}
	l := &Listing{Crumbs: []string{"Albums"}}
	for _, it := range items {
		if it.Album != nil {
			l.Entries = append(l.Entries, albumEntry(it.Album, "albums/"))
		}
	}
	return l, nil
}

func albumEntry(a *Album, prefix string) Entry {
	detail := artistNames(a.Artists)
	if len(a.ReleaseDate) >= 4 {
		detail += " · " + a.ReleaseDate[:4]
	}
	return Entry{Name: a.Name, Path: prefix + a.ID, Type: "dir", URI: a.URI, Detail: detail}
}

func (lib *Library) album(ctx context.Context, id string, crumbs []string) (*Listing, error) {
	var a Album
	if err := lib.sp.get(ctx, "/albums/"+url.PathEscape(id), nil, &a); err != nil {
		return nil, err
	}
	tracks := []Track{}
	if a.Tracks != nil {
		tracks = a.Tracks.Items
	}
	if a.Tracks != nil && a.Tracks.Total > len(tracks) {
		all, _, err := fetchAll[Track](ctx, lib.sp, "/albums/"+url.PathEscape(id)+"/tracks", nil, 50, maxTracks)
		if err != nil {
			return nil, err
		}
		tracks = all
	}
	l := &Listing{Crumbs: append(crumbs, a.Name), Context: a.URI}
	for i := range tracks {
		t := &tracks[i]
		if playable(t) {
			e := trackEntry(t)
			e.Album = a.Name
			l.Entries = append(l.Entries, e)
		}
	}
	return l, nil
}

func (lib *Library) artists(ctx context.Context) (*Listing, error) {
	l := &Listing{Crumbs: []string{"Artists"}}
	after := ""
	for len(l.Entries) < maxItems {
		q := url.Values{"type": {"artist"}, "limit": {"50"}}
		if after != "" {
			q.Set("after", after)
		}
		var res struct {
			Artists struct {
				Items   []Artist `json:"items"`
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
			} `json:"artists"`
		}
		if err := lib.sp.get(ctx, "/me/following", q, &res); err != nil {
			return nil, err
		}
		for _, a := range res.Artists.Items {
			l.Entries = append(l.Entries, Entry{Name: a.Name, Path: "artists/" + a.ID, Type: "dir", URI: a.URI})
		}
		after = res.Artists.Cursors.After
		if after == "" || len(res.Artists.Items) == 0 {
			break
		}
	}
	return l, nil
}

func (lib *Library) artist(ctx context.Context, id string) (*Listing, error) {
	var a Artist
	if err := lib.sp.get(ctx, "/artists/"+url.PathEscape(id), nil, &a); err != nil {
		return nil, err
	}
	albums, _, err := fetchAll[*Album](ctx, lib.sp, "/artists/"+url.PathEscape(id)+"/albums",
		url.Values{"include_groups": {"album,single,compilation"}}, 10, 100)
	if err != nil {
		return nil, err
	}
	// The artist itself first: plays their catalogue as a context, since top-tracks is gone from the API.
	l := &Listing{Crumbs: []string{"Artists", a.Name}, Context: a.URI}
	l.Entries = append(l.Entries, Entry{Name: "Play " + a.Name, Type: "context", URI: a.URI})
	for _, al := range albums {
		if al != nil {
			l.Entries = append(l.Entries, albumEntry(al, "artists/"+id+"/"))
		}
	}
	return l, nil
}
