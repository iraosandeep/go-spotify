import type { ReactNode } from "react";

/** `▸─ Title ── [extra] ──────` when focused, `── Title ──────` otherwise. */
export function PanelTitle({
  title,
  focused,
  extra,
}: {
  title: ReactNode;
  focused: boolean;
  extra?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-2 whitespace-nowrap">
      <span className={focused ? "text-tui-yellow" : ""}>
        {focused ? "▸─" : "──"}
      </span>
      <span
        className={focused ? "truncate font-bold text-tui-yellow" : "truncate"}
      >
        {title}
      </span>
      {extra != null && (
        <>
          <span className={focused ? "text-tui-yellow" : ""}>──</span>
          <span>{extra}</span>
        </>
      )}
      <div className="h-0 min-w-4 flex-1 border-t border-tui-fg" />
    </div>
  );
}
