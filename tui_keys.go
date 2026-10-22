package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}
	if !m.sessLoaded {
		return m, nil
	}
	if !m.sess.Configured || m.setupEdit || !m.sess.LoggedIn {
		return m.handleSetupKey(msg)
	}
	if msg.String() == "ctrl+k" {
		m.helpOpen = !m.helpOpen
		return m, nil
	}
	if m.helpOpen {
		if msg.String() == "esc" {
			m.helpOpen = false
		}
		return m, nil
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	return m.handleBrowserKey(msg)
}

func (m *tuiModel) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loggingIn {
		return m, nil
	}
	editing := !m.sess.Configured || m.setupEdit
	if editing {
		switch msg.String() {
		case "enter":
			id := strings.TrimSpace(m.setupInput.Value())
			if id == "" {
				return m, nil
			}
			if err := m.cfg.setClientID(id); err != nil {
				m.loginErr = err.Error()
				return m, nil
			}
			m.setupEdit = false
			m.loginErr = ""
			return m, m.loadSessionCmd()
		case "esc":
			if m.sess.Configured {
				m.setupEdit = false
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.setupInput, cmd = m.setupInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "enter":
		u, err := m.auth.LoginURL()
		if err != nil {
			m.loginErr = err.Error()
			return m, nil
		}
		m.loggingIn = true
		m.loginErr = ""
		if !m.noOpen {
			go openBrowser(u)
		}
		m.flash = "Open this URL to log in: " + u
		return m, nil
	case "c":
		m.setupEdit = true
		m.setupInput.SetValue("")
		m.setupInput.Focus()
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filterInput.SetValue("")
		m.filtering = false
		return m, nil
	case "enter":
		m.filtering = false
		q := m.filterInput.Value()
		if isLink(q) {
			m.filterInput.SetValue("")
			return m, m.resolveCmd(q, true)
		}
		if m.dir == "search" && strings.TrimSpace(q) != "" {
			m.search = strings.TrimSpace(q)
			m.filterInput.SetValue("")
			m.loading = true
			return m, m.loadListingCmd(false)
		}
		return m, nil
	case "down":
		m.cursor = clamp(m.cursor+1, 0, len(m.visibleRows())-1)
		return m, nil
	case "up":
		m.cursor = clamp(m.cursor-1, 0, len(m.visibleRows())-1)
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.cursor = clamp(m.cursor, 0, len(m.visibleRows())-1)
	return m, cmd
}

func (m *tuiModel) handleBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.visibleRows()
	move := func(d int) {
		if m.focus == focusBrowser {
			m.cursor = clamp(m.cursor+d, 0, len(rows)-1)
		} else {
			m.settingsRow = clamp(m.settingsRow+d, 0, len(settingRowNames)-1)
		}
	}

	switch msg.String() {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "tab", "shift+tab":
		if m.focus == focusBrowser {
			m.focus = focusSettings
		} else {
			m.focus = focusBrowser
		}
		return m, nil
	case "down", "j":
		move(1)
		return m, nil
	case "up", "k":
		move(-1)
		return m, nil
	case "pgdown":
		move(10)
		return m, nil
	case "pgup":
		move(-10)
		return m, nil
	case "g", "home":
		move(-len(rows))
		return m, nil
	case "G", "end":
		move(len(rows))
		return m, nil
	case "left", "right":
		step := 1
		if msg.String() == "left" {
			step = -1
		}
		if m.focus == focusSettings {
			return m, m.changeSetting(settingRowNames[m.settingsRow], step)
		}
		return m, m.seekBy(float64(step) * 5)
	case "shift+left", "shift+right":
		step := 30.0
		if msg.String() == "shift+left" {
			step = -30
		}
		return m, m.seekBy(step)
	case "enter":
		if m.focus == focusSettings {
			return m, m.changeSetting(settingRowNames[m.settingsRow], 1)
		}
		if m.cursor >= 0 && m.cursor < len(rows) {
			return m, m.activate(rows[m.cursor])
		}
		return m, nil
	case "esc", "backspace":
		if m.filterInput.Value() != "" {
			m.filterInput.SetValue("")
			return m, nil
		}
		if m.focus == focusSettings {
			m.focus = focusBrowser
			return m, nil
		}
		return m, m.goUp()
	case " ":
		return m, m.toggle()
	case "/":
		m.focus = focusBrowser
		m.filtering = true
		m.filterInput.Focus()
		return m, nil
	case "n":
		return m, m.doCmd("next", playerAction{})
	case "p":
		return m, m.doCmd("previous", playerAction{})
	case "+", "=":
		return m, m.changeSetting("VOL", 1)
	case "-", "_":
		return m, m.changeSetting("VOL", -1)
	case "e":
		return m, m.changeSetting("EQ", 1)
	case "s":
		return m, m.changeSetting("SHF", 1)
	case "r":
		return m, m.changeSetting("RPT", 1)
	case "d":
		return m, m.changeSetting("DEV", 1)
	case "R":
		m.reload++
		m.loading = true
		return m, m.loadListingCmd(true)
	case "L":
		_ = m.auth.Logout()
		m.lib.Forget()
		m.track, m.playing = nil, false
		return m, m.loadSessionCmd()
	case "o":
		return m, m.openSearch()
	}
	return m, nil
}
