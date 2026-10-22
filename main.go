package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed VERSION
var rawVersion string

var version = strings.TrimSpace(rawVersion)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "tui":
			runTUICommand(os.Args[2:])
			return
		case "update":
			runUpdateCommand(os.Args[2:])
			return
		}
	}
	var (
		port     = flag.Int("port", 7172, "port to listen on; must match the redirect URI registered for your Spotify app")
		clientID = flag.String("client-id", "", "Spotify app client ID (saved for next time; or set SPOTIFY_CLIENT_ID)")
		noOpen   = flag.Bool("no-open", false, "do not launch a browser")
		dev      = flag.String("dev", "", "serve the UI from this directory (e.g. web/dist) instead of the embedded copy")
		showVer  = flag.Bool("version", false, "print version and exit")
		noColor  = flag.Bool("no-color", false, "disable colour output")
		quiet    = flag.Bool("quiet", false, "suppress narration")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "gospotify %s - a terminal-styled Spotify player\n\n"+
			"usage:\n"+
			"  gospotify [flags] [spotify link]   run the browser UI\n"+
			"  gospotify tui [flags] [link]       run the terminal UI\n"+
			"  gospotify update [flags]           update to the latest release\n\n"+
			"flags for the browser UI:\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *noColor {
		f := false
		uiForcedColor = &f
	}
	uiQuiet = *quiet

	if *showVer {
		fmt.Printf("gospotify %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}
	if *dev != "" {
		if err := useDiskAssets(*dev); err != nil {
			fatal(fmt.Errorf("-dev %s: %w", *dev, err))
		}
	}

	cfg := loadConfig(configPath())
	if id := firstNonEmpty(*clientID, os.Getenv("SPOTIFY_CLIENT_ID")); id != "" {
		if err := cfg.setClientID(id); err != nil {
			uiStatus("warn", "could not save config", err.Error(), os.Stderr)
		}
	}

	// Spotify only accepts loopback redirect URIs as 127.0.0.1, and the port must match the
	// registered one exactly, so there is no walking forward to a free port here.
	addr := net.JoinHostPort("127.0.0.1", fmt.Sprint(*port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal(fmt.Errorf("port %d is taken (is gospotify already running?): %w", *port, err))
	}
	base := "http://" + addr
	auth := NewAuth(cfg, base+"/callback")
	srv := &http.Server{Handler: NewServer(cfg, auth, NewSpotify(auth)), ReadHeaderTimeout: 10 * time.Second}

	uiHeading("gospotify "+version, os.Stdout)
	uiKV("url", uiAccent(base, os.Stdout), 9, os.Stdout)
	uiKV("redirect", base+"/callback", 9, os.Stdout)
	uiKV("config", configPath(), 9, os.Stdout)
	if cfg.clientID() == "" {
		uiStatus("warn", "no client ID yet", "create an app at https://developer.spotify.com/dashboard, then paste its ID into the UI", os.Stdout)
	}
	uiHint("Spotify Premium is needed for playback · ctrl-c to stop", os.Stdout)

	open := base
	if flag.NArg() > 0 { // gospotify <spotify link>: open the UI on it.
		open += "/?open=" + url.QueryEscape(strings.Join(flag.Args(), " "))
	}
	if !*noOpen {
		go openBrowser(open)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		fmt.Print("\r")
		uiStatus("warn", "stopping", "", os.Stderr)
		go func() {
			<-stop // Second interrupt forces immediate exit
			os.Exit(130)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fatal(err)
	}
}

// runTUICommand runs `gospotify tui`: the same player and library, driven entirely from the
// terminal. It remote-controls a Spotify Connect device (phone, desktop app, speaker…) instead of
// playing audio itself, since that needs the Web Playback SDK, which only runs in a browser.
func runTUICommand(args []string) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	port := fs.Int("port", 7172, "port to listen on for the Spotify login callback; must match the redirect URI registered for your Spotify app")
	clientID := fs.String("client-id", "", "Spotify app client ID (saved for next time; or set SPOTIFY_CLIENT_ID)")
	noOpen := fs.Bool("no-open", false, "do not launch a browser to log in; print the URL instead")
	noColor := fs.Bool("no-color", false, "disable colour output")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "gospotify tui %s - the same player, driven from your terminal\n\nusage: gospotify tui [flags] [spotify link]\n\nflags:\n", version)
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	if *noColor {
		f := false
		uiForcedColor = &f
	}

	cfg := loadConfig(configPath())
	if id := firstNonEmpty(*clientID, os.Getenv("SPOTIFY_CLIENT_ID")); id != "" {
		if err := cfg.setClientID(id); err != nil {
			fatal(fmt.Errorf("could not save config: %w", err))
		}
	}

	link := strings.Join(fs.Args(), " ")
	redirectURI := "http://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(*port)) + "/callback"
	if err := RunTUI(cfg, redirectURI, *noOpen, link); err != nil {
		fatal(err)
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" {
			return x
		}
	}
	return ""
}

func openBrowser(target string) {
	if b := os.Getenv("BROWSER"); b != "" {
		if exec.Command(b, target).Start() == nil {
			return
		}
	}
	var cmds []*exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmds = []*exec.Cmd{exec.Command("open", target)}
	case "windows":
		cmds = []*exec.Cmd{exec.Command("rundll32", "url.dll,FileProtocolHandler", target)}
	default:
		cmds = []*exec.Cmd{exec.Command("xdg-open", target), exec.Command("sensible-browser", target), exec.Command("gio", "open", target)}
	}
	for _, cmd := range cmds {
		if cmd.Start() == nil {
			return
		}
	}
}

func fatal(err error) {
	uiStatus("err", err.Error(), "", os.Stderr)
	os.Exit(1)
}
