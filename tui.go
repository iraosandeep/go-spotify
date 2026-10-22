package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The terminal UI drives playback the same way the browser UI's non-SDK path does: it polls
// /me/player and sends playback commands through the Web API, moving audio between whatever
// Spotify Connect devices (phone, desktop app, speaker…) are already signed in. There is no way
// for a terminal process to become a playback device itself — that needs the Web Playback SDK,
// which only runs inside a browser.

const (
	pollInterval = 3 * time.Second
	clockTick    = 250 * time.Millisecond
)

type focusArea int

const (
	focusBrowser focusArea = iota
	focusSettings
)

type tuiRow struct {
	up    bool
	entry Entry
	n     int
}

// tuiModel is the Bubble Tea model for `gospotify tui`.
type tuiModel struct {
	cfg         *Config
	auth        *Auth
	sp          *Spotify
	lib         *Library
	player      *Player
	redirectURI string
	noOpen      bool
	openLink    string // a link given on the command line, played once logged in.

	width, height int
	quitting      bool

	sess       session
	sessLoaded bool
	loggingIn  bool
	loginErr   string
	setupInput textinput.Model
	setupEdit  bool

	dir             string
	crumbs          []string
	context         string
	notice          string
	entries         []Entry
	rows            []tuiRow
	cursor          int
	loading         bool
	loadErr         string
	reload          int
	search          string
	selectAfterLoad string

	filtering   bool
	filterInput textinput.Model

	queueName string
	queueURIs []string

	focus       focusArea
	settingsRow int
	helpOpen    bool

	track      *TrackInfo
	playing    bool
	buffering  bool
	positionMs float64
	posAt      time.Time
	shuffle    bool
	repeat     string
	deviceName string
	deviceID   string
	devices    []Device
	volumeDb   int
	eq         int
	playErr    string
	flash      string

	pendingCycleStep int
}

const (
	volMin  = -30
	volMax  = 0
	volStep = 3
)

var eqPresets = []string{"Flat", "Bass", "Treble", "Vocal", "Loud"}
var repeats = []string{"off", "context", "track"}

// RunTUI starts the terminal player. It blocks until the user quits.
func RunTUI(cfg *Config, redirectURI string, noOpen bool, link string) error {
	addr := strings.TrimPrefix(strings.TrimPrefix(redirectURI, "http://"), "https://")
	addr = strings.TrimSuffix(addr, "/callback")

	auth := NewAuth(cfg, redirectURI)
	sp := NewSpotify(auth)
	lib := NewLibrary(sp)
	player := NewPlayer(sp)

	m := newTUIModel(cfg, auth, sp, lib, player, redirectURI, noOpen, link)

	var prog *tea.Program
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port already in use (is gospotify already running?): %w", err)
	}
	srv := &http.Server{Handler: loginCallbackHandler(auth, lib, func(err error) {
		if prog != nil {
			prog.Send(loginResultMsg{err: err})
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	prog = tea.NewProgram(m, tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

func newTUIModel(cfg *Config, auth *Auth, sp *Spotify, lib *Library, player *Player, redirectURI string, noOpen bool, link string) *tuiModel {
	si := textinput.New()
	si.Placeholder = "client id"
	si.CharLimit = 200
	si.Prompt = ""
	si.Focus()

	fi := textinput.New()
	fi.CharLimit = 300
	fi.Prompt = ""

	return &tuiModel{
		cfg: cfg, auth: auth, sp: sp, lib: lib, player: player,
		redirectURI: redirectURI, noOpen: noOpen, openLink: link,
		setupInput: si, filterInput: fi,
		width: 100, height: 30,
		repeat: "off", volumeDb: -6,
	}
}

func (m *tuiModel) Init() tea.Cmd {
	return m.loadSessionCmd()
}

// --- messages ---

type sessionMsg struct{ s session }
type loginResultMsg struct{ err error }
type listingMsg struct {
	l   *Listing
	err error
}
type resolvedMsg struct {
	r        *Resolved
	autoplay bool
	err      error
}
type playerStateMsg struct {
	st  *PlayerState
	err error
}
type devicesMsg struct {
	devs []Device
	err  error
}
type actionDoneMsg struct {
	kind string
	err  error
}
type pollTickMsg struct{}
type clockTickMsg struct{}
type clearFlashMsg struct{}

func (m *tuiModel) loadSessionCmd() tea.Cmd {
	cfg, sp, redirectURI := m.cfg, m.sp, m.redirectURI
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return sessionMsg{checkSession(ctx, cfg, sp, redirectURI)}
	}
}

func (m *tuiModel) loadListingCmd(fresh bool) tea.Cmd {
	lib, dir, q := m.lib, m.dir, m.search
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		l, err := lib.List(ctx, dir, q, fresh)
		return listingMsg{l, err}
	}
}

func (m *tuiModel) resolveCmd(link string, autoplay bool) tea.Cmd {
	sp := m.sp
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r, err := Resolve(ctx, sp, link)
		return resolvedMsg{r, autoplay, err}
	}
}

func (m *tuiModel) pollCmd() tea.Cmd {
	player := m.player
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := player.State(ctx)
		return playerStateMsg{st, err}
	}
}

func (m *tuiModel) devicesCmd() tea.Cmd {
	player := m.player
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		devs, err := player.Devices(ctx)
		return devicesMsg{devs, err}
	}
}

func (m *tuiModel) doCmd(kind string, a playerAction) tea.Cmd {
	player := m.player
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return actionDoneMsg{kind, player.Do(ctx, kind, a)}
	}
}

func delay(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

func tickPoll() tea.Cmd                    { return delay(pollInterval, pollTickMsg{}) }
func tickClock() tea.Cmd                   { return delay(clockTick, clockTickMsg{}) }
func clearFlashIn(d time.Duration) tea.Cmd { return delay(d, clearFlashMsg{}) }

// --- login callback server ---

func loginCallbackHandler(auth *Auth, lib *Library, done func(error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			err := errors.New("Spotify login: " + e)
			writeCallbackPage(w, err)
			done(err)
			return
		}
		if err := auth.Exchange(r.Context(), q.Get("state"), q.Get("code")); err != nil {
			writeCallbackPage(w, err)
			done(err)
			return
		}
		lib.Forget()
		writeCallbackPage(w, nil)
		done(nil)
	})
	return mux
}

func writeCallbackPage(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	body, color := "Logged in. You can close this tab and go back to the terminal.", "#a3be8c"
	if err != nil {
		body, color = "Login failed: "+html.EscapeString(err.Error()), "#bf616a"
	}
	fmt.Fprintf(w, `<!doctype html><title>gospotify</title>`+
		`<body style="font-family:ui-monospace,monospace;background:#2e3440;color:%s;padding:2rem">%s</body>`, color, body)
}
