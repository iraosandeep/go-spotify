package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// Player wraps the Spotify Web API's playback endpoints, so the HTTP server and the terminal UI
// drive playback (and read its state) the same way.
type Player struct{ sp *Spotify }

func NewPlayer(sp *Spotify) *Player { return &Player{sp: sp} }

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

// State reads the active device's playback state, or (nil, nil) when nothing is playing anywhere.
func (p *Player) State(ctx context.Context) (*PlayerState, error) {
	var raw struct {
		Device     *Device `json:"device"`
		IsPlaying  bool    `json:"is_playing"`
		ProgressMs int     `json:"progress_ms"`
		Shuffle    bool    `json:"shuffle_state"`
		Repeat     string  `json:"repeat_state"`
		Item       *Track  `json:"item"`
	}
	if err := p.sp.get(ctx, "/me/player", url.Values{"additional_types": {"episode"}}, &raw); err != nil {
		return nil, err
	}
	if raw.Device == nil {
		return nil, nil
	}
	st := &PlayerState{Device: *raw.Device, Playing: raw.IsPlaying, PositionMs: raw.ProgressMs, Shuffle: raw.Shuffle, Repeat: raw.Repeat}
	if t := raw.Item; t != nil {
		st.Track = &TrackInfo{Name: t.Name, URI: t.URI, Artists: artistNames(t.Artists), DurationMs: t.DurationMs}
		if t.Album != nil {
			st.Track.Album = t.Album.Name
		} else if t.Show != nil {
			st.Track.Album = t.Show.Name
		}
	}
	return st, nil
}

// Devices lists the user's available Spotify Connect devices.
func (p *Player) Devices(ctx context.Context) ([]Device, error) {
	var res struct {
		Devices []Device `json:"devices"`
	}
	if err := p.sp.get(ctx, "/me/player/devices", nil, &res); err != nil {
		return nil, err
	}
	if res.Devices == nil {
		res.Devices = []Device{}
	}
	return res.Devices, nil
}

// playerAction is a playback command: the body of POST /api/player/{action}, and what the TUI
// builds directly when driving playback itself.
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

var (
	errNothingToPlay       = errors.New("nothing to play")
	errMissingDeviceID     = errors.New("missing deviceId")
	errBadRepeatValue      = errors.New("repeat must be off, context or track")
	errUnknownPlayerAction = errors.New("unknown action")
)

// Do runs a playback command against the active (or given) device.
func (p *Player) Do(ctx context.Context, action string, a playerAction) error {
	q := url.Values{}
	if a.DeviceID != "" {
		q.Set("device_id", a.DeviceID)
	}
	var (
		method = http.MethodPut
		path   string
		body   any
	)
	switch action {
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
			return errNothingToPlay
		}
		path, body = "/me/player/play", b
	case "resume":
		path = "/me/player/play"
	case "pause":
		path = "/me/player/pause"
	case "next":
		method, path = http.MethodPost, "/me/player/next"
	case "previous":
		method, path = http.MethodPost, "/me/player/previous"
	case "seek":
		path = "/me/player/seek"
		q.Set("position_ms", strconv.Itoa(max(0, a.PositionMs)))
	case "volume":
		path = "/me/player/volume"
		q.Set("volume_percent", strconv.Itoa(min(100, max(0, a.Volume))))
	case "shuffle":
		path = "/me/player/shuffle"
		q.Set("state", strconv.FormatBool(a.Shuffle))
	case "repeat":
		if a.Repeat != "off" && a.Repeat != "context" && a.Repeat != "track" {
			return errBadRepeatValue
		}
		path = "/me/player/repeat"
		q.Set("state", a.Repeat)
	case "transfer":
		if a.DeviceID == "" {
			return errMissingDeviceID
		}
		path, q = "/me/player", nil
		body = map[string]any{"device_ids": []string{a.DeviceID}, "play": a.Play}
	default:
		return errUnknownPlayerAction
	}
	return p.sp.do(ctx, method, path, q, body, nil)
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
