import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/format";

export type Status = "stopped" | "streaming" | "paused" | "buffering" | "error";
/** Spotify's repeat states: off, the whole context, or the current track. */
export type Repeat = "off" | "context" | "track";

/** Bands shown in the EQ row and drawn by the spectrum. */
export const EQ_BANDS = [
  70, 180, 320, 600, 1000, 3000, 6000, 12000, 14000, 16000,
];

// Spotify's audio is DRM-protected, so the browser can't equalise or analyse it: the presets
// shape the visualiser only.
export const EQ_PRESETS: { name: string; gains: number[] }[] = [
  { name: "Flat", gains: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0] },
  { name: "Bass", gains: [6, 5, 3, 1, 0, 0, 0, 0, 0, 0] },
  { name: "Treble", gains: [0, 0, 0, 0, 0, 1, 3, 5, 6, 6] },
  { name: "Vocal", gains: [-2, -1, 0, 2, 4, 4, 3, 1, 0, -1] },
  { name: "Loud", gains: [5, 3, 0, -1, 0, 0, 1, 3, 4, 5] },
];

export const VOL_MIN = -30;
export const VOL_MAX = 0;
export const VOL_STEP = 3;

const SDK_URL = "https://sdk.scdn.co/spotify-player.js";
const DEVICE_NAME = "GoSpotify Web";
const POLL_MS = 3000;
const REPEATS: Repeat[] = ["off", "context", "track"];

const dbToGain = (db: number) => (db <= VOL_MIN ? 0 : Math.pow(10, db / 20));
const pctToDb = (pct: number) =>
  pct <= 0
    ? VOL_MIN
    : Math.max(VOL_MIN, Math.round((20 * Math.log10(pct / 100)) / VOL_STEP) * VOL_STEP);

export type NowPlaying = {
  name: string;
  uri: string;
  artists: string;
  album: string;
  durationMs: number;
};

export type Device = {
  id: string;
  name: string;
  type: string;
  is_active: boolean;
  volume_percent: number | null;
};

/** GET /api/player: the active device's state when it isn't this page. */
type RemoteState = {
  device: Device;
  playing: boolean;
  positionMs: number;
  shuffle: boolean;
  repeat: Repeat;
  track: NowPlaying | null;
} | null;

/** What to start playing: a context (album/playlist/artist) and/or a list of track URIs. */
export type PlayRequest = { context?: string; uris?: string[]; offsetUri?: string };

type Snapshot = {
  track: NowPlaying | null;
  playing: boolean;
  loading: boolean;
  positionMs: number;
  at: number; // performance.now() when positionMs was true
  shuffle: boolean;
  repeat: Repeat;
};

const EMPTY: Snapshot = {
  track: null,
  playing: false,
  loading: false,
  positionMs: 0,
  at: 0,
  shuffle: false,
  repeat: "off",
};

function loadSdk(): Promise<void> {
  if (window.Spotify) return Promise.resolve();
  return new Promise((resolve, reject) => {
    window.onSpotifyWebPlaybackSDKReady = () => resolve();
    const s = document.createElement("script");
    s.src = SDK_URL;
    s.async = true;
    s.onerror = () => reject(new Error("Couldn't load the Spotify player"));
    document.head.appendChild(s);
  });
}

/**
 * Playback through the Spotify Web Playback SDK, which turns this page into a Spotify Connect
 * device. When another device (phone, desktop app) is playing, the same controls drive it via the
 * Web API, with state polled from the server.
 */
export function usePlayer({ enabled }: { enabled: boolean }) {
  const sdkRef = useRef<Spotify.Player | null>(null);
  const [localId, setLocalId] = useState<string | null>(null);
  const [localActive, setLocalActive] = useState(false);
  const [snap, setSnap] = useState<Snapshot>(EMPTY);
  const [remote, setRemote] = useState<Device | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [volumeDb, setVolumeDbState] = useState(-6);
  const [latency, setLatency] = useState<number | null>(null);
  const [calls, setCalls] = useState(0);
  const [, setTick] = useState(0);
  const volumeRef = useRef(volumeDb);
  volumeRef.current = volumeDb;

  /** A Web API call through the server, timing the round trip for the NET row. */
  const call = useCallback(async <T,>(url: string, body?: unknown) => {
    const t0 = performance.now();
    try {
      const out = await api<T>(url, body);
      setError(null);
      return out;
    } catch (err) {
      setError((err as Error).message);
      throw err;
    } finally {
      setLatency(Math.round(performance.now() - t0));
      setCalls((c) => c + 1);
    }
  }, []);

  const poll = useCallback(async () => {
    try {
      const st = await call<RemoteState>("/api/player");
      if (!st) {
        setRemote(null);
        setSnap((s) => ({ ...s, playing: false, loading: false }));
        return;
      }
      setRemote(st.device);
      if (st.device.volume_percent != null) setVolumeDbState(pctToDb(st.device.volume_percent));
      setSnap({
        track: st.track,
        playing: st.playing,
        loading: false,
        positionMs: st.positionMs,
        at: performance.now(),
        shuffle: st.shuffle,
        repeat: st.repeat,
      });
    } catch {
      // Shown via `error`.
    }
  }, [call]);

  // The in-page device.
  useEffect(() => {
    if (!enabled) return;
    let player: Spotify.Player | null = null;
    let cancelled = false;
    loadSdk()
      .then(() => {
        if (cancelled || !window.Spotify) return;
        player = new window.Spotify.Player({
          name: DEVICE_NAME,
          volume: dbToGain(volumeRef.current),
          getOAuthToken: (cb) => {
            api<{ accessToken: string }>("/api/token")
              .then((d) => cb(d.accessToken))
              .catch((err) => setError(err.message));
          },
        });
        player.addListener("ready", ({ device_id }) => setLocalId(device_id));
        player.addListener("not_ready", () => {
          setLocalId(null);
          setLocalActive(false);
        });
        player.addListener("initialization_error", ({ message }) =>
          setError(`Player: ${message}`),
        );
        player.addListener("authentication_error", ({ message }) =>
          setError(`Login: ${message}`),
        );
        player.addListener("account_error", () =>
          setError("Spotify Premium is required for playback"),
        );
        player.addListener("playback_error", ({ message }) =>
          setError(`Playback: ${message}`),
        );
        player.addListener("player_state_changed", (st) => {
          setLocalActive(!!st);
          if (!st) return;
          const t = st.track_window.current_track;
          setSnap({
            track: t && {
              name: t.name,
              uri: t.uri,
              artists: t.artists.map((a) => a.name).join(", "),
              album: t.album.name,
              durationMs: t.duration_ms || st.duration,
            },
            playing: !st.paused,
            loading: !!st.loading,
            positionMs: st.position,
            at: performance.now(),
            shuffle: st.shuffle,
            repeat: REPEATS[st.repeat_mode] ?? "off",
          });
        });
        void player.connect();
        sdkRef.current = player;
      })
      .catch((err) => setError(err.message));
    return () => {
      cancelled = true;
      player?.disconnect();
      sdkRef.current = null;
    };
  }, [enabled]);

  // Follow other devices when this page isn't the one playing.
  useEffect(() => {
    if (!enabled || localActive) return;
    void poll();
    const id = setInterval(poll, POLL_MS);
    return () => clearInterval(id);
  }, [enabled, localActive, poll]);

  // Redraw the clock while playing; position is interpolated from the last snapshot.
  useEffect(() => {
    if (!snap.playing) return;
    const id = setInterval(() => setTick((t) => t + 1), 250);
    return () => clearInterval(id);
  }, [snap.playing]);

  const durationMs = snap.track?.durationMs ?? 0;
  const positionMs = Math.min(
    durationMs || Infinity,
    snap.positionMs + (snap.playing ? performance.now() - snap.at : 0),
  );

  /** Run a Web API action, then refresh the remote state shortly after. */
  const action = useCallback(
    async (name: string, body?: object) => {
      await call(`/api/player/${name}`, body ?? {}).catch(() => {});
      if (!localActive) setTimeout(poll, 400);
    },
    [call, localActive, poll],
  );

  const play = useCallback(
    async (req: PlayRequest) => {
      const sdk = sdkRef.current;
      // Browsers only allow audio after a user gesture; this runs inside one.
      await sdk?.activateElement().catch(() => {});
      setSnap((s) => ({ ...s, loading: true }));
      // Prefer this page as the device; fall back to whatever is active (e.g. no Premium SDK).
      await action("play", { ...req, deviceId: localActive || !remote ? (localId ?? "") : "" });
    },
    [action, localActive, localId, remote],
  );

  const toggle = useCallback(() => {
    if (localActive) return void sdkRef.current?.togglePlay();
    if (!snap.track) return;
    setSnap((s) => ({ ...s, playing: !s.playing, positionMs: positionMs, at: performance.now() }));
    void action(snap.playing ? "pause" : "resume");
  }, [action, localActive, positionMs, snap.playing, snap.track]);

  const seek = useCallback(
    (ms: number) => {
      if (!snap.track) return;
      const to = Math.max(0, Math.min(durationMs, ms));
      setSnap((s) => ({ ...s, positionMs: to, at: performance.now() }));
      if (localActive) void sdkRef.current?.seek(to);
      else void action("seek", { positionMs: Math.round(to) });
    },
    [action, durationMs, localActive, snap.track],
  );

  const seekBy = useCallback((d: number) => seek(positionMs + d * 1000), [positionMs, seek]);

  const next = useCallback(() => {
    if (localActive) void sdkRef.current?.nextTrack();
    else void action("next");
  }, [action, localActive]);

  const previous = useCallback(() => {
    if (localActive) void sdkRef.current?.previousTrack();
    else void action("previous");
  }, [action, localActive]);

  // Volume changes are debounced for remote devices, which rate-limit the endpoint.
  const volTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const setVolumeDb = useCallback(
    (f: (db: number) => number) => {
      const db = f(volumeRef.current);
      setVolumeDbState(db);
      if (localActive || !remote) void sdkRef.current?.setVolume(dbToGain(db));
      if (!localActive && remote) {
        clearTimeout(volTimer.current);
        volTimer.current = setTimeout(
          () => void action("volume", { volume: Math.round(dbToGain(db) * 100) }),
          300,
        );
      }
    },
    [action, localActive, remote],
  );

  const setShuffle = useCallback(
    (on: boolean) => {
      setSnap((s) => ({ ...s, shuffle: on }));
      void action("shuffle", { shuffle: on });
    },
    [action],
  );

  const setRepeat = useCallback(
    (r: Repeat) => {
      setSnap((s) => ({ ...s, repeat: r }));
      void action("repeat", { repeat: r });
    },
    [action],
  );

  /** Move playback to the next available device (this page included), keeping it playing. */
  const cycleDevice = useCallback(
    async (step: number) => {
      const devices = await call<Device[]>("/api/devices").catch(() => [] as Device[]);
      if (localId && !devices.some((d) => d.id === localId))
        devices.push({ id: localId, name: DEVICE_NAME, type: "Computer", is_active: localActive, volume_percent: null });
      if (devices.length === 0) return;
      const cur = devices.findIndex((d) => d.is_active || d.id === (localActive ? localId : remote?.id));
      const target = devices[(cur + step + devices.length) % devices.length]!;
      if (target.is_active) return;
      await action("transfer", { deviceId: target.id, play: snap.playing });
      if (target.id === localId) setRemote(null);
      setTimeout(poll, 800);
    },
    [action, call, localActive, localId, poll, remote, snap.playing],
  );

  const status: Status = error && !snap.track
    ? "error"
    : !snap.track
      ? "stopped"
      : snap.loading
        ? "buffering"
        : snap.playing
          ? "streaming"
          : "paused";

  return {
    ready: !!localId,
    status,
    error,
    clearError: () => setError(null),
    track: snap.track,
    time: positionMs / 1000,
    duration: durationMs / 1000,
    shuffle: snap.shuffle,
    repeat: snap.repeat,
    volumeDb,
    setVolumeDb,
    device: localActive ? DEVICE_NAME : (remote?.name ?? (localId ? DEVICE_NAME : null)),
    remote: !localActive && !!remote,
    net: { latency, calls },
    play,
    toggle,
    seek: (s: number) => seek(s * 1000),
    seekBy,
    next,
    previous,
    setShuffle,
    setRepeat,
    cycleDevice,
  };
}
