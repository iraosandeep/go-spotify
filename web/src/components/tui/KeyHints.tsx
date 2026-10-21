export type Hint = { keys: string; label: string; action?: () => void };

/** Footer row of `[Key] Label` badges; badges are clickable for mouse/touch users. */
export function KeyHints({ hints }: { hints: Hint[] }) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {hints.map((h) => (
        <span key={h.keys} className="flex items-center gap-2">
          <button
            onClick={h.action}
            tabIndex={-1}
            className="bg-tui-badge px-2 font-bold text-tui-bright hover:bg-tui-yellow hover:text-tui-bg"
          >
            {h.keys}
          </button>
          <span>{h.label}</span>
        </span>
      ))}
    </div>
  );
}
