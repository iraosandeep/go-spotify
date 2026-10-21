package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Scopes needed for the library browser and the in-browser Web Playback SDK device.
var scopes = []string{
	"streaming",
	"user-read-email",
	"user-read-private",
	"user-read-playback-state",
	"user-modify-playback-state",
	"user-read-currently-playing",
	"user-library-read",
	"user-follow-read",
	"user-read-recently-played",
	"user-top-read",
	"playlist-read-private",
	"playlist-read-collaborative",
}

var (
	errNotLoggedIn = errors.New("not logged in")
	errNoClientID  = errors.New("no Spotify client ID configured")
)

// Auth runs the Authorization Code + PKCE flow, so no client secret is needed, and keeps the
// access token fresh.
type Auth struct {
	cfg         *Config
	redirectURI string
	accountsURL string
	client      *http.Client

	mu      sync.Mutex
	pending map[string]string // state -> PKCE verifier
	refresh sync.Mutex        // serialises token refreshes
}

func NewAuth(cfg *Config, redirectURI string) *Auth {
	return &Auth{
		cfg:         cfg,
		redirectURI: redirectURI,
		accountsURL: "https://accounts.spotify.com",
		client:      &http.Client{Timeout: 15 * time.Second},
		pending:     map[string]string{},
	}
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// LoginURL starts a login, remembering the verifier until the callback arrives.
func (a *Auth) LoginURL() (string, error) {
	id := a.cfg.clientID()
	if id == "" {
		return "", errNoClientID
	}
	verifier, state := randomString(48), randomString(16)
	sum := sha256.Sum256([]byte(verifier))
	a.mu.Lock()
	a.pending[state] = verifier
	a.mu.Unlock()

	q := url.Values{
		"client_id":             {id},
		"response_type":         {"code"},
		"redirect_uri":          {a.redirectURI},
		"code_challenge_method": {"S256"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"state":                 {state},
		"scope":                 {strings.Join(scopes, " ")},
	}
	return a.accountsURL + "/authorize?" + q.Encode(), nil
}

// Exchange completes a login started by LoginURL.
func (a *Auth) Exchange(ctx context.Context, state, code string) error {
	a.mu.Lock()
	verifier, ok := a.pending[state]
	delete(a.pending, state)
	a.mu.Unlock()
	if !ok {
		return errors.New("login expired or was started elsewhere; try again")
	}
	tok, err := a.requestToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.redirectURI},
		"client_id":     {a.cfg.clientID()},
		"code_verifier": {verifier},
	})
	if err != nil {
		return err
	}
	return a.cfg.setToken(tok)
}

// AccessToken returns a valid access token, refreshing it when it is about to expire.
func (a *Auth) AccessToken(ctx context.Context) (string, error) {
	if t := a.cfg.token(); t != nil && time.Until(t.Expiry) > time.Minute {
		return t.AccessToken, nil
	}
	a.refresh.Lock()
	defer a.refresh.Unlock()
	t := a.cfg.token()
	if t == nil || t.RefreshToken == "" {
		return "", errNotLoggedIn
	}
	if time.Until(t.Expiry) > time.Minute { // Another request refreshed it while we waited.
		return t.AccessToken, nil
	}
	fresh, err := a.requestToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.RefreshToken},
		"client_id":     {a.cfg.clientID()},
	})
	if err != nil {
		var te *tokenError
		if errors.As(err, &te) && te.Code == "invalid_grant" {
			_ = a.cfg.setToken(nil)
			return "", errNotLoggedIn
		}
		return "", err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = t.RefreshToken
	}
	if err := a.cfg.setToken(fresh); err != nil {
		uiStatus("warn", "could not save token", err.Error(), os.Stderr)
	}
	return fresh.AccessToken, nil
}

func (a *Auth) Logout() error { return a.cfg.setToken(nil) }

type tokenError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *tokenError) Error() string {
	if e.Description != "" {
		return "spotify: " + e.Description
	}
	return "spotify: " + e.Code
}

func (a *Auth) requestToken(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.accountsURL+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		te := &tokenError{}
		if json.NewDecoder(res.Body).Decode(te) != nil || te.Code == "" {
			te.Code = fmt.Sprintf("token request failed (%s)", res.Status)
		}
		return nil, te
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	return &Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}
