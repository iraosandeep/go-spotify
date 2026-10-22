package main

import (
	"context"
	"errors"
)

// session is what both the browser UI and the terminal UI need to know to decide whether to show
// a setup screen, a login prompt, or the player itself.
type session struct {
	Configured  bool   `json:"configured"`
	LoggedIn    bool   `json:"loggedIn"`
	User        *User  `json:"user,omitempty"`
	RedirectURI string `json:"redirectUri"`
	Error       string `json:"error,omitempty"`
}

func checkSession(ctx context.Context, cfg *Config, sp *Spotify, redirectURI string) session {
	out := session{Configured: cfg.clientID() != "", RedirectURI: redirectURI}
	if out.Configured && cfg.token() != nil {
		var u User
		err := sp.get(ctx, "/me", nil, &u)
		switch {
		case err == nil:
			out.LoggedIn, out.User = true, &u
		case errors.Is(err, errNotLoggedIn):
		default:
			out.LoggedIn = cfg.token() != nil
			out.Error = err.Error()
		}
	}
	return out
}
