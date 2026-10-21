import { useEffect, useRef } from "react";
import { EQ_BANDS, EQ_PRESETS } from "@/hooks/usePlayer";

const GREEN = "#a3be8c";
const YELLOW = "#ebcb8b";
const RED = "#bf616a";

// Typical music energy per band, loudest in the bass.
const BASE = [0.62, 0.58, 0.5, 0.44, 0.38, 0.3, 0.22, 0.12, 0.06, 0.04];

/** Small deterministic hash so each track gets its own "groove". */
function seedOf(s: string) {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return (h >>> 0) / 4294967295;
}

/**
 * One stacked bar per EQ band (green → yellow → red), like a terminal visualiser.
 *
 * Spotify streams are DRM-protected, so there is no audio to analyse in the page: the levels are
 * synthesised from the playback clock, a per-track seed and the EQ preset.
 */
export function Spectrum({
  active,
  seed,
  eq,
}: {
  active: boolean;
  seed: string;
  eq: number;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d")!;
    const levels = new Array(EQ_BANDS.length).fill(0);
    const r = seedOf(seed);
    const bpm = 90 + Math.round(r * 50);
    const gains = EQ_PRESETS[eq]!.gains;
    let raf = 0;

    const draw = (now: number) => {
      const dpr = window.devicePixelRatio || 1;
      const w = canvas.clientWidth;
      const h = canvas.clientHeight;
      if (canvas.width !== w * dpr || canvas.height !== h * dpr) {
        canvas.width = w * dpr;
        canvas.height = h * dpr;
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);

      const t = now / 1000;
      const beat = (t * bpm) / 60;
      const kick = Math.exp(-(beat % 1) * 5);
      const gap = 10;
      const barW = (w - gap * (EQ_BANDS.length - 1)) / EQ_BANDS.length;

      EQ_BANDS.forEach((_, i) => {
        const wobble =
          0.5 +
          0.3 * Math.sin(t * (1.3 + i * 0.37) + r * 40 + i) +
          0.2 * Math.sin(t * (3.1 + i * 0.91) + r * 17 * i);
        const punch = i < 3 ? kick * 0.35 : i > 5 ? (beat % 0.5 < 0.08 ? 0.15 : 0) : 0;
        const shaped = (BASE[i]! * wobble + punch) * Math.pow(10, gains[i]! / 20);
        const target = active ? Math.min(1, shaped) : 0;
        // Fast attack, slow fall.
        levels[i] = target > levels[i] ? target : levels[i] * 0.9;

        const barH = levels[i] * h;
        const x = i * (barW + gap);
        const segments: [number, number, string][] = [
          [0, 0.55, GREEN],
          [0.55, 0.88, YELLOW],
          [0.88, 1, RED],
        ];
        for (const [from, to, color] of segments) {
          const segH = Math.min(barH, to * h) - from * h;
          if (segH <= 0) continue;
          ctx.fillStyle = color;
          ctx.fillRect(x, h - from * h - segH, barW, segH);
        }
      });
      raf = requestAnimationFrame(draw);
    };
    raf = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(raf);
  }, [active, seed, eq]);

  return <canvas ref={canvasRef} className="block h-full w-full" />;
}
