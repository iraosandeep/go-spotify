import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Help } from "@/components/tui/Help";
import { KeyHints, type Hint } from "@/components/tui/KeyHints";
import { PanelTitle } from "@/components/tui/PanelTitle";
import { Progress } from "@/components/tui/Progress";
import {
  SETTING_ROWS,
  Settings,
  type SettingRow,
} from "@/components/tui/Settings";
import { Setup, type Session } from "@/components/tui/Setup";
import { Spectrum } from "@/components/tui/Spectrum";
import {
  EQ_PRESETS,
  VOL_MAX,
  VOL_MIN,
  VOL_STEP,
  usePlayer,
  type PlayRequest,
  type Repeat,
  type Status,
} from "@/hooks/usePlayer";
import { api, formatDuration, formatTime } from "@/lib/format";
import "./index.css";

type Entry = {
  name: string;
  path?: string;
  type: "dir" | "track" | "context";
  uri?: string;
  detail?: string;
  album?: string;
  durationMs?: number;
};

type Listing = {
  path: string;
  crumbs: string[];
  context?: string;
  notice?: string;
  entries: Entry[];
};

type Focus = "browser" | "settings";

/** GET /api/resolve: what a pasted Spotify link points at. */
type Resolved = {
  kind: string;
  uri: string;
  name?: string;
  path?: string;
  play: PlayRequest;
};

const LINK_RE = /open\.spotify\.com\/|^\s*spotify:|spotify\.link\/|spoti\.fi\//;
const isLink = (s: string) => LINK_RE.test(s);
const PENDING_LINK = "gospotify:link";

/**
 * A link the page was opened with: `?open=<link>`, or an open.spotify.com path on this host
 * (swap `open.spotify.com` for `127.0.0.1:7172` in any Spotify URL).
 */
function linkFromLocation(): string | null {
  const open = new URLSearchParams(window.location.search).get("open");
  if (open) return open;
  const path = window.location.pathname;
  if (/^\/(intl-[\w-]+\/)?(track|album|playlist|artist|episode|show)\/[A-Za-z0-9]{22}/.test(path))
    return `https://open.spotify.com${path}`;
  return null;
}

/** A list row: either the `..` parent link or a real entry. */
type Row = { kind: "up" } | { kind: "entry"; entry: Entry; n: number };

const STATUS: Record<
  Status,
  { icon: string; label: string; className: string }
> = {
  stopped: { icon: "■", label: "Stopped", className: "" },
  streaming: { icon: "●", label: "Streaming", className: "text-tui-green" },
  paused: { icon: "❚❚", label: "Paused", className: "text-tui-yellow" },
  buffering: { icon: "◌", label: "Buffering", className: "text-tui-yellow" },
  error: { icon: "✕", label: "Error", className: "text-tui-red" },
};

const REPEATS: Repeat[] = ["off", "context", "track"];
const parentOf = (p: string) => p.split("/").slice(0, -1).join("/");
const cycle = (i: number, dir: number, n: number) => (i + dir + n) % n;

export function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [loginError, setLoginError] = useState<string | null>(() =>
    new URLSearchParams(window.location.search).get("error"),
  );
  const [dir, setDir] = useState("");
  // Query sent to Spotify in Search/ (the local filter is `query`).
  const [search, setSearch] = useState("");
  const [listing, setListing] = useState<Listing | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [reload, setReload] = useState(0);
  const [query, setQuery] = useState("");
  const [filtering, setFiltering] = useState(false);
  const [cursor, setCursor] = useState(0);
  const [focus, setFocus] = useState<Focus>("browser");
  const [settingsRow, setSettingsRow] = useState(0);
  const [helpOpen, setHelpOpen] = useState(false);
  const [eq, setEq] = useState(0);
  // The listing the current track was started from, for the SRC row's queue position.
  const [queue, setQueue] = useState<{ name: string; uris: string[] } | null>(null);
  // An opened link waiting for a key press: browsers only start audio after a user gesture.
  const [pending, setPending] = useState<Resolved | null>(null);
  const [flash, setFlash] = useState<string | null>(null);

  // Name to put the cursor on once the next listing loads (the folder we just left).
  const selectAfterLoad = useRef<string | null>(null);
  const rowRefs = useRef<(HTMLLIElement | null)[]>([]);

  const loggedIn = !!session?.loggedIn;
  const player = usePlayer({ enabled: loggedIn });

  const loadSession = useCallback(() => {
    api<Session>("/api/session")
      .then(setSession)
      .catch((err) => setLoginError(err.message));
  }, []);

  useEffect(() => {
    loadSession();
    // Keep a link across the login redirect.
    const link = linkFromLocation();
    if (link) sessionStorage.setItem(PENDING_LINK, link);
    if (window.location.search || window.location.pathname !== "/")
      window.history.replaceState(null, "", "/");
  }, [loadSession]);

  useEffect(() => {
    if (!loggedIn) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    const params = new URLSearchParams({ path: dir });
    if (dir === "search" && search) params.set("q", search);
    if (reload > 0) params.set("fresh", "1");
    api<Listing>(`/api/files?${params}`)
      .then((data) => {
        if (cancelled) return;
        setListing(data);
        const name = selectAfterLoad.current;
        selectAfterLoad.current = null;
        const idx = name
          ? data.entries.findIndex((e) => e.path === name || e.uri === name)
          : -1;
        // Offset by the `..` row in subfolders; default to the first real entry.
        const offset = dir ? 1 : 0;
        setCursor(idx >= 0 ? idx + offset : data.entries.length ? offset : 0);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err.status === 401) return loadSession();
        setListing(null);
        setError(err.message);
      })
      .finally(() => !cancelled && setLoading(false));
    setQuery("");
    setFiltering(false);
    return () => {
      cancelled = true;
    };
  }, [dir, search, reload, loggedIn, loadSession]);

  const entries = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (listing?.entries ?? []).filter(
      (e) =>
        !q ||
        [e.name, e.detail, e.album].some((s) => s?.toLowerCase().includes(q)),
    );
  }, [listing, query]);

  const rows = useMemo<Row[]>(
    () => [
      ...(dir ? [{ kind: "up" } as Row] : []),
      ...entries.map((entry, i) => ({ kind: "entry", entry, n: i + 1 }) as Row),
    ],
    [dir, entries],
  );

  useEffect(() => {
    setCursor((c) => Math.min(c, Math.max(0, rows.length - 1)));
  }, [rows.length]);

  useEffect(() => {
    rowRefs.current[cursor]?.scrollIntoView({ block: "nearest" });
  }, [cursor, rows]);

  const location = ["Library", ...(listing?.crumbs ?? [])].join(" / ");
  const section = listing?.crumbs[0] ?? "Library";

  const goUp = useCallback(() => {
    if (!dir) return;
    setFlash(null);
    selectAfterLoad.current = dir;
    if (dir === "search") setSearch("");
    setDir(parentOf(dir));
  }, [dir]);

  const activate = (row: Row | undefined) => {
    if (!row) return;
    setFlash(null);
    setPending(null);
    if (row.kind === "up") return goUp();
    const { entry } = row;
    if (entry.type === "dir" && entry.path) return setDir(entry.path);
    if (!listing || !entry.uri) return;
    const uris = listing.entries
      .filter((e) => e.type === "track" && e.uri)
      .map((e) => e.uri!);
    setQueue({ name: section, uris });
    if (entry.type === "context") return void player.play({ context: entry.uri });
    // Albums and playlists play in their context, so Spotify drives next/shuffle/repeat;
    // other lists (liked, search…) are sent as a queue of tracks.
    void player.play(
      listing.context
        ? { context: listing.context, offsetUri: entry.uri }
        : { uris, offsetUri: entry.uri },
    );
  };

  /** Open a Spotify link: show its folder and, when this runs on a user gesture, play it. */
  const openLink = async (link: string, autoplay: boolean) => {
    setFlash("Opening link…");
    try {
      const r = await api<Resolved>(`/api/resolve?url=${encodeURIComponent(link.trim())}`);
      const label = r.name || `${r.kind} ${r.uri.split(":").pop()}`;
      if (r.path) {
        selectAfterLoad.current = r.kind === "track" ? r.uri : null;
        setSearch("");
        setDir(r.path);
        setFocus("browser");
      }
      if (autoplay) {
        setPending(null);
        setFlash(null);
        setQueue({ name: label, uris: r.play.uris ?? [] });
        await player.play(r.play);
      } else {
        setPending(r);
        setFlash(`Press Space to play ${label}`);
      }
    } catch (err) {
      setFlash(`✕ ${(err as Error).message}`);
    }
  };

  // Links the page was opened with (or carried across login) open once logged in.
  useEffect(() => {
    if (!loggedIn) return;
    const link = sessionStorage.getItem(PENDING_LINK);
    sessionStorage.removeItem(PENDING_LINK);
    if (link) void openLink(link, false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loggedIn]);

  // Pasting a Spotify link anywhere plays it.
  const onPaste = useRef<(e: ClipboardEvent) => void>(() => {});
  onPaste.current = (e) => {
    const text = e.clipboardData?.getData("text") ?? "";
    if (!loggedIn || !isLink(text)) return;
    e.preventDefault();
    setFiltering(false);
    setQuery("");
    void openLink(text, true);
  };
  useEffect(() => {
    const handler = (e: ClipboardEvent) => onPaste.current(e);
    window.addEventListener("paste", handler);
    return () => window.removeEventListener("paste", handler);
  }, []);

  const toggle = () => {
    if (player.track) return player.toggle();
    // Nothing loaded yet: play the link waiting for a gesture, or the folder's album/playlist.
    if (pending) return void openLink(pending.uri, true);
    if (listing?.context) {
      setQueue({ name: section, uris: [] });
      void player.play({ context: listing.context });
    }
  };

  const openSearch = () => {
    setFocus("browser");
    if (dir !== "search") {
      setSearch("");
      setDir("search");
    }
    setTimeout(() => setFiltering(true));
  };

  const changeSetting = (row: SettingRow, step: number) => {
    switch (row) {
      case "VOL":
        player.setVolumeDb((v) =>
          Math.max(VOL_MIN, Math.min(VOL_MAX, v + step * VOL_STEP)),
        );
        break;
      case "EQ":
        setEq((e) => cycle(e, step, EQ_PRESETS.length));
        break;
      case "SHF":
        player.setShuffle(!player.shuffle);
        break;
      case "RPT":
        player.setRepeat(
          REPEATS[cycle(REPEATS.indexOf(player.repeat), step, REPEATS.length)]!,
        );
        break;
      case "DEV":
        void player.cycleDevice(step);
        break;
      case "SRC":
        setSearch("");
        setDir("");
        setFocus("browser");
        break;
    }
  };

  const lastVolume = useRef(-6);
  const toggleMute = () =>
    player.setVolumeDb((v) => {
      if (v > VOL_MIN) {
        lastVolume.current = v;
        return VOL_MIN;
      }
      return lastVolume.current;
    });

  const toggleFullscreen = () => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void document.documentElement.requestFullscreen();
  };

  const toggleFocus = () =>
    setFocus((f) => (f === "browser" ? "settings" : "browser"));

  const logout = async () => {
    await api("/api/logout", {}).catch(() => {});
    window.location.reload();
  };

  // Keyboard driven, like the terminal app it mimics. Kept in a ref so the listener sees fresh state.
  const onKey = useRef<(e: KeyboardEvent) => void>(() => {});
  onKey.current = (e) => {
    // The setup overlay handles its own keys.
    if (!loggedIn) return;
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === "k") {
      e.preventDefault();
      setHelpOpen((o) => !o);
      return;
    }
    if (helpOpen) {
      if (e.key === "Escape") setHelpOpen(false);
      return;
    }
    if (mod || e.altKey) return;

    if (filtering) {
      if (e.key === "Escape") {
        setQuery("");
        setFiltering(false);
      } else if (e.key === "Enter") {
        setFiltering(false);
        if (isLink(query)) {
          void openLink(query, true);
          setQuery("");
        } else if (dir === "search" && query.trim()) {
          // In Search/, Enter sends the typed text to Spotify instead of filtering.
          setSearch(query.trim());
          setQuery("");
        }
      } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setCursor((c) =>
          Math.max(
            0,
            Math.min(rows.length - 1, c + (e.key === "ArrowDown" ? 1 : -1)),
          ),
        );
      }
      return;
    }

    const page = 10;
    const move = (d: number) => {
      e.preventDefault();
      if (focus === "browser")
        setCursor((c) => Math.max(0, Math.min(rows.length - 1, c + d)));
      else
        setSettingsRow((r) =>
          Math.max(0, Math.min(SETTING_ROWS.length - 1, r + d)),
        );
    };

    switch (e.key) {
      case "Tab":
        e.preventDefault();
        toggleFocus();
        return;
      case "ArrowDown":
      case "j":
        return move(1);
      case "ArrowUp":
      case "k":
        return move(-1);
      case "PageDown":
        return move(page);
      case "PageUp":
        return move(-page);
      case "g":
      case "Home":
        return move(-rows.length);
      case "G":
      case "End":
        return move(rows.length);
      case "ArrowLeft":
      case "ArrowRight": {
        e.preventDefault();
        const d = e.key === "ArrowRight" ? 1 : -1;
        if (focus === "settings") changeSetting(SETTING_ROWS[settingsRow]!, d);
        else player.seekBy(d * (e.shiftKey ? 30 : 5));
        return;
      }
      case "Enter":
        e.preventDefault();
        if (focus === "settings") changeSetting(SETTING_ROWS[settingsRow]!, 1);
        else activate(rows[cursor]);
        return;
      case "Escape":
      case "Backspace":
        e.preventDefault();
        if (query) setQuery("");
        else if (focus === "settings") setFocus("browser");
        else goUp();
        return;
      case " ":
        e.preventDefault();
        toggle();
        return;
      case "/":
        e.preventDefault();
        setFocus("browser");
        setFiltering(true);
        return;
      case "n":
        return player.next();
      case "p":
        return player.previous();
      case "f":
        return toggleFullscreen();
      case "m":
        return toggleMute();
      case "+":
      case "=":
        return changeSetting("VOL", 1);
      case "-":
      case "_":
        return changeSetting("VOL", -1);
      case "e":
        return changeSetting("EQ", 1);
      case "s":
        return changeSetting("SHF", 1);
      case "r":
        return changeSetting("RPT", 1);
      case "d":
        return changeSetting("DEV", 1);
      case "R":
        return setReload((r) => r + 1);
      case "L":
        return void logout();
      case "o":
        e.preventDefault();
        return openSearch();
    }
  };

  useEffect(() => {
    const handler = (e: KeyboardEvent) => onKey.current(e);
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const status = STATUS[player.status];
  const track = player.track;
  const queueIdx =
    track && queue ? queue.uris.indexOf(track.uri) : -1;

  const hints: Hint[] =
    focus === "browser"
      ? [
          { keys: "Esc", label: "Back", action: goUp },
          {
            keys: "Enter",
            label: "Open",
            action: () => activate(rows[cursor]),
          },
          { keys: "Space", label: "Play / Pause", action: toggle },
          { keys: "←/→", label: "Seek", action: () => player.seekBy(5) },
          { keys: "n/p", label: "Next / Prev", action: player.next },
          { keys: "/", label: "Filter", action: () => setFiltering(true) },
          { keys: "f", label: "Fullscreen", action: toggleFullscreen },
          { keys: "o", label: "Search", action: openSearch },
          { keys: "Tab/Shift+Tab", label: "Focus", action: toggleFocus },
          { keys: "Ctrl+K", label: "Help", action: () => setHelpOpen(true) },
        ]
      : [
          {
            keys: "Esc",
            label: "Back to browser",
            action: () => setFocus("browser"),
          },
          {
            keys: "↑/↓",
            label: "Select",
            action: () =>
              setSettingsRow((r) => cycle(r, 1, SETTING_ROWS.length)),
          },
          {
            keys: "←/→",
            label: "Change",
            action: () => changeSetting(SETTING_ROWS[settingsRow]!, 1),
          },
          { keys: "Space", label: "Play / Pause", action: toggle },
          { keys: "Tab/Shift+Tab", label: "Focus", action: toggleFocus },
          { keys: "Ctrl+K", label: "Help", action: () => setHelpOpen(true) },
        ];

  const notice = flash ?? player.error ?? listing?.notice ?? null;

  return (
    <div className="flex min-h-dvh flex-col gap-1 px-4 py-4 sm:px-9 sm:py-6 md:h-dvh md:overflow-hidden">
      {/* Header */}
      <header className="shrink-0">
        <div className="flex justify-between gap-4">
          <span className="tracking-[0.5em] text-tui-green">GOSPOTIFY</span>
          <span>[{focus === "browser" ? "Browser" : "Settings"}]</span>
        </div>
        <div className="truncate text-tui-yellow">
          ♫{" "}
          {track
            ? `${track.name} · ${track.artists}${track.album ? ` · ${track.album}` : ""}`
            : "No track loaded"}
        </div>
        <div className="flex justify-between gap-4">
          <span>
            {formatTime(player.time)} /{" "}
            {track ? formatTime(player.duration) : "00:00"}
          </span>
          <button
            onClick={toggle}
            tabIndex={-1}
            className={status.className}
          >
            {status.icon} {status.label}
            {player.remote && player.device ? ` on ${player.device}` : ""}
          </button>
        </div>
      </header>

      <div className="h-18 shrink-0 pt-2">
        <Spectrum
          active={player.status === "streaming"}
          seed={track?.uri ?? ""}
          eq={eq}
        />
      </div>

      <div className="shrink-0">
        <Progress
          time={player.time}
          duration={player.duration}
          buffered={track ? [[0, player.time]] : []}
          loaded={!!track}
          onSeek={player.seek}
        />
      </div>

      {/* Browser + Settings */}
      <div className="flex min-h-0 flex-1 flex-col gap-4 md:flex-row md:gap-10">
        <section
          className="flex min-h-[45vh] min-w-0 flex-1 flex-col md:min-h-0"
          onClick={() => setFocus("browser")}
        >
          <PanelTitle
            title={location}
            focused={focus === "browser"}
            extra={
              rows.length > 0 ? `[${cursor + 1}/${rows.length}]` : undefined
            }
          />
          {(filtering || query) && (
            <div className="flex gap-1 pl-4">
              <span className="text-tui-yellow">
                {dir === "search" ? "search or link:" : "/"}
              </span>
              {filtering ? (
                <input
                  autoFocus
                  value={query}
                  onChange={(e) => {
                    setQuery(e.target.value);
                    setCursor(dir ? 1 : 0);
                  }}
                  onBlur={() => setFiltering(false)}
                  className="flex-1 bg-transparent caret-tui-yellow outline-none"
                  spellCheck={false}
                />
              ) : (
                <span>
                  {query}{" "}
                  <span className="text-tui-dim">
                    ({entries.length} match{entries.length === 1 ? "" : "es"})
                  </span>
                </span>
              )}
            </div>
          )}
          <ul className="min-h-0 flex-1 overflow-y-auto">
            {loading && <li className="pl-4 text-tui-dim">Loading…</li>}
            {notice && (
              <li className={`pl-4 ${notice.startsWith("✕") ? "text-tui-red" : "text-tui-yellow"}`}>
                {notice.startsWith("✕") ? "" : "! "}
                {notice}
              </li>
            )}
            {error && <li className="pl-4 text-tui-red">✕ {error}</li>}
            {!loading && !error && rows.length === 0 && (
              <li className="pl-4 text-tui-dim">Nothing here</li>
            )}
            {!loading &&
              rows.map((row, i) => {
                const selected = i === cursor;
                const playing =
                  row.kind === "entry" &&
                  !!track &&
                  row.entry.uri === track.uri;
                const width = String(entries.length).length;
                return (
                  <li
                    key={row.kind === "up" ? ".." : (row.entry.path ?? row.entry.uri ?? row.n)}
                    ref={(el) => {
                      rowRefs.current[i] = el;
                    }}
                    onClick={() => {
                      setCursor(i);
                      activate(row);
                    }}
                    className={`flex cursor-pointer gap-2 whitespace-pre hover:bg-tui-badge/40 ${
                      selected
                        ? "font-bold text-tui-yellow"
                        : playing
                          ? "text-tui-green"
                          : ""
                    }`}
                  >
                    <span className="w-6 shrink-0 text-tui-yellow">
                      {selected ? ">" : " "}
                      {playing ? "▶" : " "}
                    </span>
                    {row.kind === "up" ? (
                      <span>{" ".repeat(width + 2)}../</span>
                    ) : (
                      <>
                        <span
                          className="shrink-0 text-right"
                          style={{ width: `${width + 1}ch` }}
                        >
                          {row.n}.
                        </span>
                        <span className="truncate">
                          <span
                            className={
                              selected || playing
                                ? ""
                                : row.entry.type === "dir"
                                  ? "text-tui-blue"
                                  : row.entry.type === "context"
                                    ? "text-tui-green"
                                    : ""
                            }
                          >
                            {row.entry.type === "context" ? "▷ " : ""}
                            {row.entry.name}
                            {row.entry.type === "dir" ? "/" : ""}
                          </span>
                          {(row.entry.detail || row.entry.durationMs) && (
                            <span className={selected ? "" : "text-tui-dim"}>
                              {row.entry.detail ? ` · ${row.entry.detail}` : ""}
                              {row.entry.durationMs
                                ? ` · ${formatDuration(row.entry.durationMs)}`
                                : ""}
                            </span>
                          )}
                        </span>
                      </>
                    )}
                  </li>
                );
              })}
          </ul>
        </section>

        <aside
          className="shrink-0 md:w-75"
          onClick={() => setFocus("settings")}
        >
          <Settings
            focused={focus === "settings"}
            row={settingsRow}
            source={queue?.name ?? section}
            queuePos={
              queue && queueIdx >= 0 ? `${queueIdx + 1}/${queue.uris.length}` : null
            }
            volumeDb={player.volumeDb}
            eq={eq}
            shuffle={player.shuffle}
            repeat={player.repeat}
            device={player.device}
            net={player.net}
            user={session?.user?.display_name ?? null}
            onChange={(row, step) => {
              setSettingsRow(SETTING_ROWS.indexOf(row));
              changeSetting(row, step);
            }}
          />
        </aside>
      </div>

      <footer className="shrink-0 pt-1">
        <KeyHints hints={hints} />
      </footer>

      {helpOpen && <Help onClose={() => setHelpOpen(false)} />}
      {session && !session.loggedIn && (
        <Setup session={session} error={loginError} onSaved={loadSession} />
      )}
    </div>
  );
}

export default App;
