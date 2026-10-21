package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Token is a Spotify OAuth token as saved on disk.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// Config holds the Spotify app's client ID and the logged-in user's token.
type Config struct {
	mu       sync.Mutex
	path     string
	ClientID string `json:"client_id,omitempty"`
	Token    *Token `json:"token,omitempty"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "go-spotify", "config.json")
}

func loadConfig(path string) *Config {
	cfg := &Config{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, cfg)
	}
	return cfg
}

// save writes the config; the token is a credential, so the file is private to the user.
func (c *Config) save() error {
	if c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(c.path, b, 0o600)
}

func (c *Config) token() *Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Token
}

func (c *Config) setToken(t *Token) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Token = t
	return c.save()
}

func (c *Config) setClientID(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ClientID != id {
		c.ClientID = id
		c.Token = nil // A token belongs to the app that issued it.
	}
	return c.save()
}

func (c *Config) clientID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ClientID
}
