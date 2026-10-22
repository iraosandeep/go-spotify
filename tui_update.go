package main

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var settingRowNames = []string{"SRC", "VOL", "EQ", "SHF", "RPT", "DEV"}

func isLink(s string) bool {
	s = strings.TrimSpace(s)
	if _, _, ok := parseLink(s); ok {
		return true
	}
	return shortRe.MatchString(s)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func dbToPct(db int) int {
	if db <= volMin {
		return 0
	}
	return int(math.Round(math.Pow(10, float64(db)/20) * 100))
}

func pctToDb(pct int) int {
	if pct <= 0 {
		return volMin
	}
	db := int(math.Round(20*math.Log10(float64(pct)/100)/volStep)) * volStep
	return clamp(db, volMin, volMax)
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case sessionMsg:
		wasLoggedIn := m.sessLoaded && m.sess.LoggedIn
		m.sess, m.sessLoaded, m.loggingIn = msg.s, true, false
		if msg.s.LoggedIn && !wasLoggedIn {
			cmds := []tea.Cmd{m.loadListingCmd(false), m.pollCmd(), tickClock()}
			if m.openLink != "" {
				link := m.openLink
				m.openLink = ""
				cmds = append(cmds, m.resolveCmd(link, true))
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case loginResultMsg:
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			return m, nil
		}
		m.loginErr = ""
		return m, m.loadSessionCmd()

	case listingMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr, m.entries, m.crumbs, m.context, m.notice = msg.err.Error(), nil, nil, "", ""
			return m, nil
		}
		m.loadErr = ""
		m.entries, m.crumbs, m.context, m.notice = msg.l.Entries, msg.l.Crumbs, msg.l.Context, msg.l.Notice
		idx := -1
		if m.selectAfterLoad != "" {
			for i, e := range m.entries {
				if e.Path == m.selectAfterLoad || e.URI == m.selectAfterLoad {
					idx = i
					break
				}
			}
		}
		m.selectAfterLoad = ""
		offset := 0
		if m.dir != "" {
			offset = 1
		}
		switch {
		case idx >= 0:
			m.cursor = idx + offset
		case len(m.entries) > 0:
			m.cursor = offset
		default:
			m.cursor = 0
		}
		return m, nil

	case resolvedMsg:
		if msg.err != nil {
			m.flash = "✕ " + msg.err.Error()
			return m, clearFlashIn(4 * time.Second)
		}
		r := msg.r
		var cmds []tea.Cmd
		if r.Path != "" {
			if r.Kind == "track" {
				m.selectAfterLoad = r.URI
			}
			cmds = append(cmds, m.setDir(r.Path))
		}
		if msg.autoplay {
			label := r.Name
			if label == "" {
				label = r.Kind
			}
			m.queueName, m.queueURIs = label, r.Play.URIs
			m.flash = ""
			m.buffering = true
			cmds = append(cmds, m.playCmd(playerAction{Context: r.Play.Context, URIs: r.Play.URIs, OffsetURI: r.Play.OffsetURI}))
		}
		return m, tea.Batch(cmds...)

	case playerStateMsg:
		if msg.err != nil {
			m.playErr = msg.err.Error()
		} else {
			m.playErr = ""
			m.buffering = false
			if msg.st == nil {
				m.track, m.playing = nil, false
			} else {
				m.track = msg.st.Track
				m.playing = msg.st.Playing
				m.positionMs, m.posAt = float64(msg.st.PositionMs), time.Now()
				m.shuffle, m.repeat = msg.st.Shuffle, msg.st.Repeat
				m.deviceName, m.deviceID = msg.st.Device.Name, msg.st.Device.ID
				if msg.st.Device.Volume != nil {
					m.volumeDb = pctToDb(*msg.st.Device.Volume)
				}
			}
		}
		return m, tickPoll()

	case devicesMsg:
		m.devices = msg.devs
		step := m.pendingCycleStep
		m.pendingCycleStep = 0
		if step == 0 || msg.err != nil || len(msg.devs) == 0 {
			return m, nil
		}
		cur := -1
		for i, d := range msg.devs {
			if d.IsActive || d.ID == m.deviceID {
				cur = i
				break
			}
		}
		n := len(msg.devs)
		target := msg.devs[((cur+step)%n+n)%n]
		if target.IsActive {
			return m, nil
		}
		return m, m.doCmd("transfer", playerAction{DeviceID: target.ID, Play: m.playing})

	case actionDoneMsg:
		var cmds []tea.Cmd
		if msg.err != nil {
			m.flash = "✕ " + msg.err.Error()
			cmds = append(cmds, clearFlashIn(4*time.Second))
		}
		d := 400 * time.Millisecond
		if msg.kind == "transfer" {
			d = 800 * time.Millisecond
		}
		cmds = append(cmds, delay(d, pollTickMsg{}))
		return m, tea.Batch(cmds...)

	case pollTickMsg:
		return m, m.pollCmd()

	case clockTickMsg:
		return m, tickClock()

	case clearFlashMsg:
		m.flash = ""
		return m, nil
	}
	return m, nil
}

// --- navigation / playback helpers ---

func parentOf(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i]
}

func (m *tuiModel) setDir(path string) tea.Cmd {
	m.dir = path
	m.filterInput.SetValue("")
	m.filtering = false
	m.loading = true
	m.loadErr = ""
	return m.loadListingCmd(false)
}

func (m *tuiModel) goUp() tea.Cmd {
	if m.dir == "" {
		return nil
	}
	m.selectAfterLoad = m.dir
	if m.dir == "search" {
		m.search = ""
	}
	return m.setDir(parentOf(m.dir))
}

func (m *tuiModel) section() string {
	if len(m.crumbs) > 0 {
		return m.crumbs[0]
	}
	return "Library"
}

func (m *tuiModel) playCmd(a playerAction) tea.Cmd {
	m.buffering = true
	return m.doCmd("play", a)
}

func (m *tuiModel) activate(row tuiRow) tea.Cmd {
	m.flash = ""
	if row.up {
		return m.goUp()
	}
	e := row.entry
	if e.Type == "dir" && e.Path != "" {
		return m.setDir(e.Path)
	}
	if e.URI == "" {
		return nil
	}
	var uris []string
	for _, x := range m.entries {
		if x.Type == "track" && x.URI != "" {
			uris = append(uris, x.URI)
		}
	}
	m.queueName, m.queueURIs = m.section(), uris
	if e.Type == "context" {
		return m.playCmd(playerAction{Context: e.URI})
	}
	if m.context != "" {
		return m.playCmd(playerAction{Context: m.context, OffsetURI: e.URI})
	}
	return m.playCmd(playerAction{URIs: uris, OffsetURI: e.URI})
}

func (m *tuiModel) toggle() tea.Cmd {
	if m.track != nil {
		was := m.playing
		m.playing = !was
		m.posAt = time.Now()
		if was {
			return m.doCmd("pause", playerAction{})
		}
		return m.doCmd("resume", playerAction{})
	}
	if m.context != "" {
		m.queueName, m.queueURIs = m.section(), nil
		return m.playCmd(playerAction{Context: m.context})
	}
	return nil
}

func (m *tuiModel) currentPositionMs() float64 {
	if m.track == nil {
		return 0
	}
	pos := m.positionMs
	if m.playing {
		pos += float64(time.Since(m.posAt).Milliseconds())
	}
	if dur := float64(m.track.DurationMs); dur > 0 && pos > dur {
		pos = dur
	}
	if pos < 0 {
		pos = 0
	}
	return pos
}

func (m *tuiModel) seekBy(deltaSec float64) tea.Cmd {
	if m.track == nil {
		return nil
	}
	to := m.currentPositionMs() + deltaSec*1000
	if to < 0 {
		to = 0
	}
	if dur := float64(m.track.DurationMs); dur > 0 && to > dur {
		to = dur
	}
	m.positionMs, m.posAt = to, time.Now()
	return m.doCmd("seek", playerAction{PositionMs: int(to)})
}

func (m *tuiModel) changeSetting(row string, step int) tea.Cmd {
	switch row {
	case "VOL":
		m.volumeDb = clamp(m.volumeDb+step*volStep, volMin, volMax)
		return m.doCmd("volume", playerAction{Volume: dbToPct(m.volumeDb)})
	case "EQ":
		n := len(eqPresets)
		m.eq = ((m.eq+step)%n + n) % n
	case "SHF":
		m.shuffle = !m.shuffle
		return m.doCmd("shuffle", playerAction{Shuffle: m.shuffle})
	case "RPT":
		n := len(repeats)
		i := indexOf(repeats, m.repeat)
		m.repeat = repeats[((i+step)%n+n)%n]
		return m.doCmd("repeat", playerAction{Repeat: m.repeat})
	case "DEV":
		m.pendingCycleStep = step
		return m.devicesCmd()
	case "SRC":
		return m.setDir("")
	}
	return nil
}

func (m *tuiModel) openSearch() tea.Cmd {
	m.focus = focusBrowser
	var cmd tea.Cmd
	if m.dir != "search" {
		m.search = ""
		cmd = m.setDir("search")
	}
	m.filtering = true
	m.filterInput.Focus()
	return cmd
}
