# gospotify

A terminal-styled Spotify player. It is a single Go binary with two front ends — a browser UI and a real
terminal UI — and it looks and drives like [vstream](https://github.com/iraosandeep/go-v-stream): a
library browser, a spectrum visualiser, a seek line, a Settings panel, and full keyboard control.

```bash
gospotify         # browser UI, with in-browser playback via the Web Playback SDK
gospotify tui     # terminal UI, remote-controlling a device you're already signed into
```

The Go side talks to the Spotify Web API (OAuth with PKCE, library, search, playback control) for both.
The **browser UI** turns the page itself into a Spotify Connect device through the Web Playback SDK, so
audio plays right there, as well as being able to control your phone or desktop app. The **terminal UI**
can't do that — the Web Playback SDK only runs inside a browser — so it always remote-controls whichever
Spotify Connect device (phone, desktop app, speaker…) is already playing, the same way the browser UI
does when it isn't the active device. Logging in still opens a browser once, for Spotify's OAuth consent
screen; everything else in `tui` mode stays in the terminal.

**Requirements:** Spotify Premium (needed for playback, and for the app owner since Spotify's 2026 rules).

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/iraosandeep/go-spotify/main/install.sh | sh
```

This downloads the binary for your OS/arch from the latest GitHub release into `~/.local/bin` (or set
`INSTALL_DIR`; pin a version with `VERSION=0.1.0`). Builds exist for macOS, Linux, Windows and FreeBSD.

Or build from source (Go 1.25+ and [Bun](https://bun.sh) for the UI bundle):

```bash
make build
install -d ~/.local/bin && install gospotify ~/.local/bin/
```

## Update

```bash
gospotify update           # checks and installs the latest release
gospotify update -check    # just checks, doesn't install
gospotify update -version 0.2.0   # install a specific version
```

It downloads the matching binary from the same GitHub releases install.sh uses, verifies it against the
release's checksum, and replaces the running binary in place. If it was installed to a location you don't
own (e.g. `/usr/local/bin` via sudo), run `sudo gospotify update` instead.

## Setup

1. Create an app at <https://developer.spotify.com/dashboard>. Tick **Web API** and **Web Playback SDK**.
2. Add the redirect URI `http://127.0.0.1:7172/callback`.
3. Run it:

```bash
gospotify -client-id <your client id>   # or paste the ID into the UI on first run
gospotify                               # afterwards
```

The client ID and your token are saved to `~/.config/go-spotify/config.json` (mode 0600).

| Flag | Default | |
| --- | --- | --- |
| `-port` | `7172` | Must match the redirect URI registered for your app |
| `-client-id` | | Spotify client ID (or `SPOTIFY_CLIENT_ID`) |
| `-no-open` | | Don't launch a browser |
| `-dev DIR` | | Serve the UI from `DIR` (e.g. `web/dist`) instead of the embedded copy |

`gospotify tui` takes the same `-port`, `-client-id` and `-no-open` (here, it just prints the login URL
instead of opening it), plus `-no-color`. It shares the same config file, so logging in once works for
both front ends.

## Play any Spotify link

Any `open.spotify.com` URL (track, album, playlist, artist, episode, show), `spotify:` URI or
`spotify.link` short link works:

- **Paste it anywhere** in the UI (`Ctrl/Cmd+V`). It plays right away.
- **Type it** into the filter or search box (`/` or `o`) and press Enter.
- **Change the host:** replace `open.spotify.com` with `127.0.0.1:7172`, e.g.
  `http://127.0.0.1:7172/playlist/37i9dQZF1DX5trt9i14X7j`. Press Space to start, because browsers only
  play audio after a key press or click.
- **From the shell:** `./gospotify https://open.spotify.com/playlist/37i9dQZF1DX5trt9i14X7j`

Albums, playlists and artists open as folders. A track opens in its album, with playback continuing
through the album. Spotify hides its own editorial playlists (IDs starting `37i9`) from apps, so those
show a single "Play playlist" row, but they still play.

## Library

```
Liked Songs/  Playlists/  Albums/  Artists/<artist>/<album>  Recently Played/  Top Tracks/  Search/
```

Albums, playlists and artists play in their Spotify context, so next/prev, shuffle and repeat follow
Spotify. Other lists (liked, search, recent) are queued as tracks from the one you pick.

## Keys

| Keys | Action |
| --- | --- |
| `↑/↓` `j/k` `PgUp/PgDn` `g/G` | Move cursor |
| `Enter` / `Esc` `Backspace` | Open / back |
| `/` | Filter the folder (in `Search/`, Enter searches Spotify) |
| `o` | Search Spotify, or type a link |
| `Ctrl/Cmd+V` | Paste a Spotify link to play it |
| `Space` | Play / pause |
| `←/→` (`Shift` = 30s) | Seek 5s |
| `n` / `p` | Next / previous track |
| `f` · `m` · `+/-` | Fullscreen · mute · volume |
| `e` · `s` · `r` · `d` | Cycle EQ · shuffle · repeat (off/all/one) · playback device |
| `R` · `L` | Reload folder · log out |
| `Tab` | Switch focus browser ↔ settings (`↑/↓` select, `←/→` change) |
| `Ctrl+K` | Help |

Keys are the same in `gospotify tui`, minus `f` (fullscreen) and `m` (mute), which don't apply to a
terminal. There's no special paste shortcut either — pasting into the `/` or `o` prompt (most terminals
paste on `Cmd/Ctrl+V` or a middle click) works the same as typing a link. `Ctrl+C` or `q` quits.

## Differences from vstream

Spotify streams are DRM-protected, so the page can't read or process the audio:

- The **spectrum** is synthesised from the playback clock and the track. It does not show real audio levels.
- **EQ** presets shape the visualiser only.
- **SPD** (speed) is replaced by **DEV**, which moves playback between your Spotify devices.
- **NET** shows Web API latency instead of bytes streamed.

With a Development Mode app, Spotify only lists tracks of playlists you own or collaborate on. Other
playlists show a single "Play" row, which plays the whole playlist.

## Development

```
main.go       flags, startup narration, browser launch, graceful shutdown
auth.go       Authorization Code + PKCE login, token refresh
spotify.go    Web API client (paging, 429 retry) and object types
library.go    the library as a folder tree (/api/files)
resolve.go    Spotify links → what to open and play (/api/resolve)
player.go     playback state and commands, shared by the server and the terminal UI
session.go    configured/logged-in/user status, shared by the server and the terminal UI
server.go     routes, the embedded browser UI
tui*.go       the terminal UI (Bubble Tea): model/update, key handling, rendering
update.go     `gospotify update`: fetch, verify and install the latest release in place
web/          React UI, bundled by Bun into web/dist (embedded via go:embed)
```

```bash
make dev               # rebuild the UI and run with it served from disk
make test              # go test ./...
make dist              # cross-compile linux/darwin/windows/freebsd into dist/
make publish 0.2.0     # bump VERSION, commit, tag; push the tag to trigger the GitHub release
```

`web/dist` is committed so `go build` works without Bun; run `make web` after changing the UI.
```
