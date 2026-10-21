package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Spotify is a small Web API client: just the endpoints this app uses.
type Spotify struct {
	auth   *Auth
	base   string
	client *http.Client
}

func NewSpotify(auth *Auth) *Spotify {
	return &Spotify{auth: auth, base: "https://api.spotify.com/v1", client: &http.Client{Timeout: 20 * time.Second}}
}

// APIError is a non-2xx answer from the Web API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("spotify: %s (%d)", e.Message, e.Status)
}

func statusOf(err error) int {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// do calls the Web API, decoding a JSON answer into out (if non-nil). It honours Retry-After on 429.
func (s *Spotify) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	u := s.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	for attempt := 0; ; attempt++ {
		token, err := s.auth.AccessToken(ctx)
		if err != nil {
			return err
		}
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := s.client.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			res.Body.Close()
			wait, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if err := sleep(ctx, time.Duration(min(max(wait, 1), 10))*time.Second); err != nil {
				return err
			}
			continue
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return apiError(res)
		}
		if out == nil || res.StatusCode == http.StatusNoContent {
			return nil
		}
		b, err := io.ReadAll(res.Body)
		if err != nil || len(bytes.TrimSpace(b)) == 0 {
			return err
		}
		return json.Unmarshal(b, out)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func apiError(res *http.Response) error {
	var body struct {
		Error struct {
			Message string `json:"message"`
			Reason  string `json:"reason"`
		} `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	msg := body.Error.Message
	if msg == "" {
		msg = strings.ToLower(http.StatusText(res.StatusCode))
	}
	if body.Error.Reason == "PREMIUM_REQUIRED" {
		msg = "Spotify Premium is required for playback"
	}
	return &APIError{Status: res.StatusCode, Message: msg}
}

func (s *Spotify) get(ctx context.Context, path string, q url.Values, out any) error {
	return s.do(ctx, http.MethodGet, path, q, nil, out)
}

// Page is an offset-paged Web API list.
type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// fetchAll reads an offset-paged list, up to max items. After the first page reveals the total,
// the remaining pages are fetched a few at a time and stitched back together in order.
func fetchAll[T any](ctx context.Context, s *Spotify, path string, q url.Values, pageSize, max int) ([]T, int, error) {
	page := func(offset int) (*Page[T], error) {
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		pq.Set("limit", strconv.Itoa(pageSize))
		pq.Set("offset", strconv.Itoa(offset))
		p := &Page[T]{}
		return p, s.get(ctx, path, pq, p)
	}
	first, err := page(0)
	if err != nil {
		return nil, 0, err
	}
	total := min(first.Total, max)
	n := (total + pageSize - 1) / pageSize
	if n <= 1 {
		return first.Items, first.Total, nil
	}
	rest := make([]*Page[T], n)
	errs := make([]error, n)
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rest[i], errs[i] = page(i * pageSize)
		}(i)
	}
	wg.Wait()
	items := first.Items
	for i := 1; i < n; i++ {
		if errs[i] != nil {
			return nil, 0, errs[i]
		}
		items = append(items, rest[i].Items...)
	}
	return items, first.Total, nil
}

// Web API objects, trimmed to the fields the UI shows.

type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URI  string `json:"uri"`
}

type Album struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	URI         string       `json:"uri"`
	AlbumType   string       `json:"album_type"`
	ReleaseDate string       `json:"release_date"`
	TotalTracks int          `json:"total_tracks"`
	Artists     []Artist     `json:"artists"`
	Tracks      *Page[Track] `json:"tracks,omitempty"`
}

type Track struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URI        string   `json:"uri"`
	Type       string   `json:"type"`
	DurationMs int      `json:"duration_ms"`
	IsLocal    bool     `json:"is_local"`
	Artists    []Artist `json:"artists"`
	Album      *Album   `json:"album,omitempty"`
	Show       *struct {
		Name string `json:"name"`
	} `json:"show,omitempty"`
}

type Playlist struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URI   string `json:"uri"`
	Owner struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"owner"`
	// "items" since the February 2026 API changes, "tracks" before.
	Items  *struct{ Total int } `json:"items,omitempty"`
	Tracks *struct{ Total int } `json:"tracks,omitempty"`
}

func (p Playlist) total() int {
	if p.Items != nil {
		return p.Items.Total
	}
	if p.Tracks != nil {
		return p.Tracks.Total
	}
	return -1
}

// PlaylistItem holds a track or episode; the field was renamed from "track" to "item" in 2026.
type PlaylistItem struct {
	Item  *Track `json:"item"`
	Track *Track `json:"track"`
}

func (p PlaylistItem) track() *Track {
	if p.Item != nil {
		return p.Item
	}
	return p.Track
}

type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

func artistNames(as []Artist) string {
	names := make([]string, 0, len(as))
	for _, a := range as {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}
