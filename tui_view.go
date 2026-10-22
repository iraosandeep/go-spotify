package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	rw "github.com/mattn/go-runewidth"
)

// Nord-inspired palette, matching the browser UI's colours (web/src/index.css).
var (
	tuiColorFg     = lipgloss.Color("#d8dee9")
	tuiColorBright = lipgloss.Color("#eceff4")
	tuiColorDim    = lipgloss.Color("#7b88a1")
	tuiColorBadge  = lipgloss.Color("#4c566a")
	tuiColorYellow = lipgloss.Color("#ebcb8b")
	tuiColorGreen  = lipgloss.Color("#a3be8c")
	tuiColorRed    = lipgloss.Color("#bf616a")

	styleYellow = lipgloss.NewStyle().Foreground(tuiColorYellow)
	styleGreen  = lipgloss.NewStyle().Foreground(tuiColorGreen)
	styleRed    = lipgloss.NewStyle().Foreground(tuiColorRed)
	styleDim    = lipgloss.NewStyle().Foreground(tuiColorDim)
)

func formatTime(sec float64) string {
	if sec < 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
		sec = 0
	}
	s := int(sec)
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func formatDuration(ms int) string { return formatTime(float64(ms) / 1000) }

func bracket(s string) string { return "[" + styleYellow.Render(s) + "]" }

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

func repeatLabel(r string) string {
	switch r {
	case "context":
		return "All"
	case "track":
		return "One"
	default:
		return "Off"
	}
}

func justify(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func panelTitle(title string, focused bool, extra string, width int) string {
	mark, style := "──", lipgloss.NewStyle()
	if focused {
		mark, style = "▸─", lipgloss.NewStyle().Foreground(tuiColorYellow).Bold(true)
	}
	markStyle := lipgloss.NewStyle()
	if focused {
		markStyle = markStyle.Foreground(tuiColorYellow)
	}
	line := markStyle.Render(mark) + " " + style.Render(rw.Truncate(title, width-10, "…"))
	if extra != "" {
		line += " " + markStyle.Render("──") + " " + extra
	}
	pad := width - lipgloss.Width(line) - 1
	if pad < 0 {
		pad = 0
	}
	return line + " " + strings.Repeat("─", pad)
}

// --- spectrum (synthesised: Spotify streams are DRM-protected, so there is no audio to analyse) ---

var spectrumBase = []float64{0.62, 0.58, 0.5, 0.44, 0.38, 0.3, 0.22, 0.12, 0.06, 0.04}

func seedOf(s string) float64 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return float64(h) / 4294967295
}

func (m *tuiModel) spectrumLine() string {
	n := len(spectrumBase)
	if m.track == nil || !m.playing {
		return styleDim.Render(strings.Repeat("▁ ", n))
	}
	seed := seedOf(m.track.URI)
	bpm := 90 + seed*50
	t := float64(time.Now().UnixMilli()) / 1000
	beat := t * bpm / 60
	kick := math.Exp(-math.Mod(beat, 1) * 5)
	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for i := 0; i < n; i++ {
		fi := float64(i)
		wobble := 0.5 + 0.3*math.Sin(t*(1.3+fi*0.37)+seed*40+fi) + 0.2*math.Sin(t*(3.1+fi*0.91)+seed*17*fi)
		punch := 0.0
		switch {
		case i < 3:
			punch = kick * 0.35
		case i > 5:
			if math.Mod(beat, 0.5) < 0.08 {
				punch = 0.15
			}
		}
		level := math.Min(1, math.Max(0, spectrumBase[i]*wobble+punch))
		ch := blocks[int(level*float64(len(blocks)-1))]
		color := tuiColorGreen
		switch {
		case level > 0.88:
			color = tuiColorRed
		case level > 0.55:
			color = tuiColorYellow
		}
		b.WriteString(lipgloss.NewStyle().Foreground(color).Render(string(ch)))
		b.WriteString(" ")
	}
	return b.String()
}

func (m *tuiModel) progressLine(w int) string {
	barWidth := w - 1
	if barWidth < 10 {
		barWidth = 10
	}
	dur := 0.0
	if m.track != nil {
		dur = float64(m.track.DurationMs)
	}
	filled := 0
	if dur > 0 {
		filled = clamp(int(m.currentPositionMs()/dur*float64(barWidth)), 0, barWidth-1)
	}
	left := strings.Repeat("─", filled)
	right := strings.Repeat("─", barWidth-filled-1)
	return styleDim.Render(left) + styleYellow.Render("●") + styleDim.Render(right)
}

func (m *tuiModel) statusParts() (string, string, lipgloss.Style) {
	switch {
	case m.playErr != "" && m.track == nil:
		return "✕", "Error", styleRed
	case m.track == nil:
		return "■", "Stopped", lipgloss.NewStyle()
	case m.buffering:
		return "◌", "Buffering", styleYellow
	case m.playing:
		return "●", "Streaming", styleGreen
	default:
		return "❚❚", "Paused", styleYellow
	}
}

func (m *tuiModel) headerLines(w int) []string {
	focusLabel := "[Browser]"
	if m.focus == focusSettings {
		focusLabel = "[Settings]"
	}
	title := styleGreen.Render(strings.Join(strings.Split("GOSPOTIFY", ""), " "))
	l1 := justify(title, focusLabel, w)

	trackLine := "No track loaded"
	if m.track != nil {
		trackLine = m.track.Name + " · " + m.track.Artists
		if m.track.Album != "" {
			trackLine += " · " + m.track.Album
		}
	}
	l2 := styleYellow.Render("♫ " + rw.Truncate(trackLine, w-2, "…"))

	dur := 0.0
	if m.track != nil {
		dur = float64(m.track.DurationMs) / 1000
	}
	timeStr := formatTime(m.currentPositionMs()/1000) + " / " + formatTime(dur)
	icon, label, style := m.statusParts()
	status := style.Render(icon + " " + label)
	if m.track != nil && m.deviceName != "" {
		status += " on " + m.deviceName
	}
	l3 := justify(timeStr, status, w)

	lines := []string{l1, l2, l3}
	notice := m.flash
	if notice == "" {
		notice = m.notice
	}
	if notice != "" {
		st := styleYellow
		if strings.HasPrefix(notice, "✕") {
			st = styleRed
		}
		lines = append(lines, st.Render(rw.Truncate(notice, w, "…")))
	}
	return lines
}

func (m *tuiModel) footerLine(w int) string {
	var hints []string
	if m.focus == focusBrowser {
		hints = []string{"Esc Back", "Enter Open", "Space Play/Pause", "←/→ Seek", "n/p Next/Prev", "/ Filter", "o Search", "Tab Focus", "Ctrl+K Help"}
	} else {
		hints = []string{"Esc Back", "↑/↓ Select", "←/→ Change", "Space Play/Pause", "Tab Focus", "Ctrl+K Help"}
	}
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(tuiColorBright)
	var parts []string
	for _, h := range hints {
		sp := strings.SplitN(h, " ", 2)
		parts = append(parts, keyStyle.Render(sp[0])+" "+sp[1])
	}
	return rw.Truncate(strings.Join(parts, "   "), w, "")
}

// --- browser panel ---

func (m *tuiModel) visibleRows() []tuiRow {
	q := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	var rows []tuiRow
	if m.dir != "" {
		rows = append(rows, tuiRow{up: true})
	}
	n := 0
	for _, e := range m.entries {
		if q != "" {
			hay := strings.ToLower(e.Name + " " + e.Detail + " " + e.Album)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		n++
		rows = append(rows, tuiRow{entry: e, n: n})
	}
	return rows
}

func (m *tuiModel) locationTitle() string {
	return strings.Join(append([]string{"Library"}, m.crumbs...), " / ")
}

func (m *tuiModel) renderRow(row tuiRow, i, numWidth, width int) string {
	cursorMark, playMark := " ", " "
	selected := i == m.cursor
	if selected {
		cursorMark = ">"
	}
	playing := !row.up && row.entry.URI != "" && m.track != nil && row.entry.URI == m.track.URI
	if playing {
		playMark = "▶"
	}
	var body string
	if row.up {
		body = strings.Repeat(" ", numWidth+2) + "../"
	} else {
		e := row.entry
		name := e.Name
		if e.Type == "dir" {
			name += "/"
		}
		if e.Type == "context" {
			name = "▷ " + name
		}
		detail := e.Detail
		if e.DurationMs > 0 {
			if detail != "" {
				detail += " · "
			}
			detail += formatDuration(e.DurationMs)
		}
		if detail != "" {
			detail = " · " + detail
		}
		body = fmt.Sprintf("%*d.", numWidth, row.n) + " " + name + detail
	}
	line := rw.Truncate(cursorMark+playMark+" "+body, width, "…")
	switch {
	case selected:
		return lipgloss.NewStyle().Bold(true).Foreground(tuiColorYellow).Render(line)
	case playing, !row.up && row.entry.Type == "context":
		return styleGreen.Render(line)
	case !row.up && row.entry.Type == "dir":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#81a1c1")).Render(line)
	}
	return line
}

func (m *tuiModel) browserLines(rows []tuiRow, width, height int) []string {
	if height < 1 {
		height = 1
	}
	if len(rows) == 0 {
		msg := "Nothing here"
		switch {
		case m.loading:
			msg = "Loading…"
		case m.loadErr != "":
			msg = "✕ " + m.loadErr
		}
		style := styleDim
		if m.loadErr != "" {
			style = styleRed
		}
		return []string{style.Render(msg)}
	}
	start := 0
	if len(rows) > height {
		start = clamp(m.cursor-height/2, 0, len(rows)-height)
	}
	end := min(start+height, len(rows))
	numWidth := len(fmt.Sprint(len(rows)))
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(rows[i], i, numWidth, width))
	}
	return lines
}

func (m *tuiModel) renderBrowserPanel(w, h int) string {
	rows := m.visibleRows()
	extra := ""
	if len(rows) > 0 {
		extra = fmt.Sprintf("[%d/%d]", m.cursor+1, len(rows))
	}
	lines := []string{panelTitle(m.locationTitle(), m.focus == focusBrowser, extra, w)}
	h--
	if m.filtering || m.filterInput.Value() != "" {
		prefix := "/"
		if m.dir == "search" {
			prefix = "search or link:"
		}
		var right string
		if m.filtering {
			right = m.filterInput.View()
		} else {
			matches := len(rows)
			if m.dir != "" {
				matches--
			}
			right = m.filterInput.Value() + styleDim.Render(fmt.Sprintf(" (%d matches)", matches))
		}
		lines = append(lines, styleYellow.Render(prefix)+" "+right)
		h--
	}
	lines = append(lines, m.browserLines(rows, w, h)...)
	return strings.Join(lines, "\n")
}

// --- settings panel ---

func settingRow(name, value string, selected bool) string {
	marker, nameStyle := "  ", lipgloss.NewStyle().Bold(true).Foreground(tuiColorBright)
	if selected {
		marker, nameStyle = "▸ ", lipgloss.NewStyle().Bold(true).Foreground(tuiColorYellow)
	}
	return marker + nameStyle.Render(fmt.Sprintf("%-4s", name)) + value
}

func volBar(db int) string {
	if db <= volMin {
		return bracket("MUTE")
	}
	pct := float64(db-volMin) / float64(volMax-volMin)
	width := 10
	filled := clamp(int(pct*float64(width)), 0, width)
	bar := styleGreen.Render(strings.Repeat("█", filled)) + lipgloss.NewStyle().Foreground(tuiColorBadge).Render(strings.Repeat("─", width-filled))
	sign := ""
	if db >= 0 {
		sign = "+"
	}
	return bar + fmt.Sprintf(" %s%ddB", sign, db)
}

func (m *tuiModel) renderSettingsPanel(w int) string {
	lines := []string{panelTitle("Settings", m.focus == focusSettings, "", w)}
	sel := func(name string) bool {
		return m.focus == focusSettings && settingRowNames[m.settingsRow] == name
	}
	src := m.queueName
	if src == "" {
		src = m.section()
	}
	qpos := ""
	if m.track != nil && len(m.queueURIs) > 0 {
		if idx := indexOf(m.queueURIs, m.track.URI); idx >= 0 {
			qpos = fmt.Sprintf(" %d/%d", idx+1, len(m.queueURIs))
		}
	}
	dev := m.deviceName
	if dev == "" {
		dev = "Connecting…"
	}
	lines = append(lines,
		settingRow("SRC", bracket(rw.Truncate(src, w-10, "…"))+qpos, sel("SRC")),
		settingRow("VOL", volBar(m.volumeDb), sel("VOL")),
		settingRow("EQ", bracket(eqPresets[m.eq]), sel("EQ")),
		settingRow("SHF", bracket(onOff(m.shuffle)), sel("SHF")),
		settingRow("RPT", bracket(repeatLabel(m.repeat)), sel("RPT")),
		settingRow("DEV", bracket(rw.Truncate(dev, w-10, "…")), sel("DEV")),
	)
	return strings.Join(lines, "\n")
}

// --- overlays ---

func boxStyle(w int) lipgloss.Style {
	if w > 76 {
		w = 76
	}
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(tuiColorFg).Padding(1, 2).Width(w)
}

func (m *tuiModel) renderSetup(w int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(tuiColorYellow).Render("Connect Spotify") + "\n\n")
	editing := !m.sess.Configured || m.setupEdit
	if editing {
		b.WriteString(styleGreen.Render("1. Create an app") + "\n")
		b.WriteString("   Open developer.spotify.com/dashboard -> Create app, tick Web API and Web Playback SDK.\n\n")
		b.WriteString(styleGreen.Render("2. Add this redirect URI") + "\n")
		b.WriteString("   " + styleYellow.Render(m.redirectURI) + "\n\n")
		b.WriteString(styleGreen.Render("3. Paste the app's Client ID") + "\n")
		b.WriteString("   " + styleYellow.Render(">") + " " + m.setupInput.View() + "\n")
	} else {
		b.WriteString("♫ Log in to stream your library here.\n")
		b.WriteString(styleDim.Render("Playback needs Spotify Premium.") + "\n\n")
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(tuiColorYellow).Render("> ✓ Log in with Spotify") + "\n")
	}
	if m.loginErr != "" {
		b.WriteString("\n" + styleRed.Render("✕ "+m.loginErr) + "\n")
	}
	if m.flash != "" {
		b.WriteString("\n" + styleYellow.Render(m.flash) + "\n")
	}
	if m.loggingIn {
		b.WriteString("\n" + styleYellow.Render("◌ Waiting for Spotify login in your browser…") + "\n")
	}
	b.WriteString("\n")
	if editing {
		hint := "Enter Save"
		if m.sess.Configured {
			hint += "    Esc Cancel"
		}
		b.WriteString(styleDim.Render(hint))
	} else {
		b.WriteString(styleDim.Render("Enter Log in    c Change client ID"))
	}
	return boxStyle(w).Render(b.String())
}

var helpSections = []struct {
	name string
	rows [][2]string
}{
	{"Browser", [][2]string{
		{"↑/↓  j/k", "Move cursor"},
		{"PgUp/PgDn", "Page up / down"},
		{"g / G", "First / last"},
		{"Enter", "Open folder / play track"},
		{"Esc  Backspace", "Back to parent folder"},
		{"/", "Filter this folder (search Spotify in Search/)"},
		{"o", "Search Spotify (or type / paste a link)"},
		{"R", "Reload this folder"},
	}},
	{"Playback", [][2]string{
		{"Space", "Play / Pause"},
		{"←/→", "Seek ±5s (Shift ±30s)"},
		{"n / p", "Next / previous track"},
		{"+/-", "Volume"},
		{"e  s  r", "Cycle EQ (visualiser) / shuffle / repeat"},
		{"d", "Move playback to the next Spotify Connect device"},
	}},
	{"General", [][2]string{
		{"Tab/Shift+Tab", "Switch focus Browser ↔ Settings"},
		{"L", "Log out of Spotify"},
		{"Ctrl+K", "Toggle this help"},
		{"Ctrl+C / q", "Quit"},
	}},
}

func (m *tuiModel) renderHelp(w int) string {
	var b strings.Builder
	b.WriteString(panelTitle("Help", true, "", min(60, w-6)) + "\n\n")
	for _, s := range helpSections {
		b.WriteString(styleGreen.Render(s.name) + "\n")
		for _, kv := range s.rows {
			b.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(tuiColorYellow).Render(fmt.Sprintf("%-16s", kv[0])) + kv[1] + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(styleDim.Render("Esc or Ctrl+K to close"))
	return boxStyle(w).Render(b.String())
}

// --- top level ---

func (m *tuiModel) renderMain(w, h int) string {
	lines := m.headerLines(w)
	lines = append(lines, "", m.spectrumLine(), "", m.progressLine(w), "")

	rightWidth := 32
	if w < 70 {
		rightWidth = 0
	}
	leftWidth := w
	if rightWidth > 0 {
		leftWidth = w - rightWidth - 2
	}
	panelHeight := h - len(lines) - 2
	if panelHeight < 5 {
		panelHeight = 5
	}

	left := m.renderBrowserPanel(leftWidth, panelHeight)
	if rightWidth > 0 {
		right := m.renderSettingsPanel(rightWidth)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right))
	} else {
		lines = append(lines, left)
	}
	lines = append(lines, "", m.footerLine(w))
	return strings.Join(lines, "\n")
}

func (m *tuiModel) View() string {
	if m.quitting {
		return ""
	}
	w, h := m.width, m.height
	if w < 40 {
		w = 40
	}
	if h < 16 {
		h = 16
	}
	if !m.sessLoaded {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, styleDim.Render("Loading…"))
	}
	if !m.sess.Configured || m.setupEdit || !m.sess.LoggedIn {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, m.renderSetup(w-4))
	}
	if m.helpOpen {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, m.renderHelp(w-4))
	}
	return m.renderMain(w, h)
}
