import { PanelTitle } from "./PanelTitle";

const SECTIONS: [string, [string, string][]][] = [
  [
    "Browser",
    [
      ["↑/↓  j/k", "Move cursor"],
      ["PgUp/PgDn", "Page up / down"],
      ["g / G", "First / last"],
      ["Enter", "Open folder / play track"],
      ["Esc  Backspace", "Back to parent folder"],
      ["/", "Filter this folder (search Spotify in Search/)"],
      ["o", "Search Spotify (or type / paste a link)"],
      ["Ctrl+V", "Paste a Spotify link anywhere to play it"],
      ["R", "Reload this folder"],
    ],
  ],
  [
    "Playback",
    [
      ["Space", "Play / Pause"],
      ["←/→", "Seek ±5s (Shift ±30s)"],
      ["n / p", "Next / previous track"],
      ["f", "Fullscreen"],
      ["+/-  m", "Volume / mute"],
      ["e  s  r", "Cycle EQ (visualiser) / shuffle / repeat"],
      ["d", "Move playback to the next device"],
    ],
  ],
  [
    "General",
    [
      ["Tab/Shift+Tab", "Switch focus Browser ↔ Settings"],
      ["L", "Log out of Spotify"],
      ["Ctrl+K", "Toggle this help"],
    ],
  ],
];

export function Help({ onClose }: { onClose: () => void }) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-tui-bg/80 p-6"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl space-y-3 border border-tui-fg bg-tui-bg p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <PanelTitle title="Help" focused />
        {SECTIONS.map(([name, rows]) => (
          <div key={name}>
            <div className="text-tui-green">{name}</div>
            {rows.map(([k, v]) => (
              <div key={k} className="flex gap-4">
                <span className="w-40 shrink-0 font-bold text-tui-yellow">
                  {k}
                </span>
                <span>{v}</span>
              </div>
            ))}
          </div>
        ))}
        <div className="text-tui-dim">Esc or Ctrl+K to close</div>
      </div>
    </div>
  );
}
