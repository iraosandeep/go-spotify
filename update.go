package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// `gospotify update` fetches the matching binary from the project's GitHub releases (the same ones
// install.sh installs from) and replaces the currently running binary with it.

func updateRepo() string {
	if r := os.Getenv("GOSPOTIFY_REPO"); r != "" {
		return r
	}
	return "iraosandeep/go-spotify"
}

func runUpdateCommand(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	ver := fs.String("version", "", "install this version instead of the latest (e.g. 0.2.0)")
	check := fs.Bool("check", false, "only check whether an update is available; don't install it")
	noColor := fs.Bool("no-color", false, "disable colour output")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "gospotify update %s - update gospotify to the latest release\n\nusage: gospotify update [flags]\n\nflags:\n", version)
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if *noColor {
		f := false
		uiForcedColor = &f
	}

	client := &http.Client{Timeout: 30 * time.Second}
	repo := updateRepo()

	uiStatus("", "checking for updates…", "current v"+version, os.Stdout)
	target := strings.TrimPrefix(strings.TrimSpace(*ver), "v")
	explicit := target != ""
	if !explicit {
		v, err := latestReleaseVersion(client, repo)
		if err != nil {
			fatal(fmt.Errorf("could not check for updates: %w", err))
		}
		target = v
	}

	switch {
	case !explicit && target == version:
		uiStatus("ok", "already up to date", "v"+version, os.Stdout)
		return
	case explicit && target == version:
		uiStatus("ok", "reinstalling", "v"+target, os.Stdout)
	default:
		uiStatus("ok", "update available", "v"+version+" → v"+target, os.Stdout)
	}
	if *check {
		if target != version {
			fmt.Fprintf(os.Stdout, "\nRun `gospotify update` to install v%s.\n", target)
		}
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fatal(fmt.Errorf("could not locate the running binary: %w", err))
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	asset := assetName(target)
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repo, target, asset)
	tmp := exe + ".new"
	defer os.Remove(tmp)

	uiStatus("", "downloading "+asset+"…", "", os.Stdout)
	if err := downloadFile(client, url, tmp); err != nil {
		fatal(fmt.Errorf("download failed: %w", err))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		fatal(err)
	}

	if err := verifyChecksum(client, repo, target, asset, tmp); err != nil {
		uiStatus("warn", "could not verify checksum", err.Error(), os.Stdout)
	}

	if err := replaceBinary(exe, tmp); err != nil {
		fatal(fmt.Errorf("could not install the update (try `sudo gospotify update`, or re-run install.sh): %w", err))
	}
	uiStatus("ok", "updated to v"+target, exe, os.Stdout)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func latestReleaseVersion(client *http.Client, repo string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: %s", res.Status)
	}
	var rel githubRelease
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.TagName, "v"), nil
}

func assetName(ver string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("gospotify-%s-%s-%s%s", ver, runtime.GOOS, runtime.GOARCH, ext)
}

func downloadFile(client *http.Client, url, dest string) error {
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, res.Status)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, res.Body)
	return err
}

// verifyChecksum checks the download against the release's checksums.txt, when there is one.
func verifyChecksum(client *http.Client, repo, ver, asset, path string) error {
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/checksums.txt", repo, ver)
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("no checksums.txt in release v%s", ver)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var want string
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == asset {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%s not listed in checksums.txt", asset)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// replaceBinary swaps the running executable for the new one. On Unix, renaming over a running
// binary works fine (the process keeps its open inode). Windows won't allow that, so the running
// file is renamed out of the way first, which it does allow.
func replaceBinary(target, tmp string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(tmp, target)
	}
	old := target + ".old"
	_ = os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(old, target)
		return err
	}
	_ = os.Remove(old) // best effort; may still be locked until this process exits
	return nil
}
