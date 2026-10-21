import { useRef } from "react";

/** Full-width seek line with a dot at the playhead; buffered ranges drawn brighter. */
export function Progress({
  time,
  duration,
  buffered,
  loaded,
  onSeek,
}: {
  time: number;
  duration: number;
  buffered: [number, number][];
  loaded: boolean;
  onSeek: (t: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const pct = duration > 0 ? Math.min(100, (time / duration) * 100) : 0;

  const seekTo = (clientX: number) => {
    const rect = ref.current?.getBoundingClientRect();
    if (!rect || !duration) return;
    onSeek(((clientX - rect.left) / rect.width) * duration);
  };

  return (
    <div
      ref={ref}
      className="relative h-4 cursor-pointer select-none"
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        seekTo(e.clientX);
      }}
      onPointerMove={(e) => e.buttons === 1 && seekTo(e.clientX)}
    >
      <div
        className={`absolute inset-x-0 top-1/2 h-0.5 -translate-y-1/2 ${loaded ? "bg-tui-badge" : "bg-tui-fg"}`}
      />
      {duration > 0 &&
        buffered.map(([s, e], i) => (
          <div
            key={i}
            className="absolute top-1/2 h-0.5 -translate-y-1/2 bg-tui-fg"
            style={{
              left: `${(s / duration) * 100}%`,
              width: `${((e - s) / duration) * 100}%`,
            }}
          />
        ))}
      <div
        className="absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-tui-yellow"
        style={{ left: `max(5px, ${pct}%)` }}
      />
    </div>
  );
}
