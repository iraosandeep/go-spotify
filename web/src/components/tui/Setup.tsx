import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/format";
import { KeyHints } from "./KeyHints";
import { PanelTitle } from "./PanelTitle";

export type Session = {
  configured: boolean;
  loggedIn: boolean;
  user?: { id: string; display_name: string };
  redirectUri: string;
  error?: string;
};

/** First run: connect a Spotify app (client ID), then log in. Can't be dismissed. */
export function Setup({
  session,
  error,
  onSaved,
}: {
  session: Session;
  error: string | null;
  onSaved: () => void;
}) {
  const [editing, setEditing] = useState(!session.configured);
  const [clientId, setClientId] = useState("");
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const login = () => {
    setBusy(true);
    window.location.href = "/login";
  };

  const save = async () => {
    if (!clientId.trim()) return;
    setBusy(true);
    setSaveError(null);
    try {
      await api("/api/config", { clientId: clientId.trim() });
      setEditing(false);
      onSaved();
    } catch (err) {
      setSaveError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const onKey = useRef<(e: KeyboardEvent) => void>(() => {});
  onKey.current = (e) => {
    if (busy) return;
    if (editing) {
      if (e.key === "Enter") void save();
      else if (e.key === "Escape" && session.configured) setEditing(false);
      return;
    }
    if (e.key === "Enter") login();
    else if (e.key === "c") {
      e.preventDefault();
      setEditing(true);
    }
  };

  useEffect(() => {
    const handler = (e: KeyboardEvent) => onKey.current(e);
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const shown = saveError ?? error ?? session.error ?? null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-tui-bg/80 p-4">
      <div className="flex w-full max-w-2xl flex-col gap-1 border border-tui-fg bg-tui-bg p-4">
        <PanelTitle title="Connect Spotify" focused />
        {editing ? (
          <>
            <div className="text-tui-green">1. Create an app</div>
            <div className="pl-3">
              Open{" "}
              <a
                className="text-tui-blue underline"
                href="https://developer.spotify.com/dashboard"
                target="_blank"
                rel="noreferrer"
              >
                developer.spotify.com/dashboard
              </a>{" "}
              → Create app, tick <span className="text-tui-yellow">Web API</span> and{" "}
              <span className="text-tui-yellow">Web Playback SDK</span>.
            </div>
            <div className="text-tui-green">2. Add this redirect URI</div>
            <div className="select-all pl-3 text-tui-yellow">{session.redirectUri}</div>
            <div className="text-tui-green">3. Paste the app's Client ID</div>
            <div className="flex gap-1 pl-3">
              <span className="text-tui-yellow">›</span>
              <input
                autoFocus
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
                placeholder="client id"
                className="flex-1 bg-transparent caret-tui-yellow outline-none placeholder:text-tui-dim"
                spellCheck={false}
              />
            </div>
          </>
        ) : (
          <>
            <div>
              ♫ Log in to stream your library here.{" "}
              <span className="text-tui-dim">Playback needs Spotify Premium.</span>
            </div>
            <button
              tabIndex={-1}
              onClick={login}
              className="flex gap-2 text-left font-bold text-tui-yellow"
            >
              <span>{">"}</span>
              <span>✓ Log in with Spotify</span>
            </button>
          </>
        )}
        {shown && <div className="text-tui-red">✕ {shown}</div>}
        {busy && <div className="text-tui-yellow">◌ Working…</div>}
        <div className="pt-2">
          <KeyHints
            hints={
              editing
                ? [
                    { keys: "Enter", label: "Save", action: () => void save() },
                    ...(session.configured
                      ? [{ keys: "Esc", label: "Cancel", action: () => setEditing(false) }]
                      : []),
                  ]
                : [
                    { keys: "Enter", label: "Log in", action: login },
                    { keys: "c", label: "Change client ID", action: () => setEditing(true) },
                  ]
            }
          />
        </div>
      </div>
    </div>
  );
}
