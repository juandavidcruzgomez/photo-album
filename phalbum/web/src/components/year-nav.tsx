import { CalendarDays } from "lucide-react"

import type { YearCount } from "@/api/types"
import { useIsDesktop } from "@/hooks/use-media-query"
import { cn } from "@/lib/utils"

interface YearNavProps {
  years: YearCount[]
  current: number | null
  onJump: (year: number) => void
  hidden?: boolean
}

/** Jump to a year: a rail on the right edge on desktop, a floating button on phones. */
export function YearNav({ years, current, onJump, hidden }: YearNavProps) {
  const desktop = useIsDesktop()
  if (years.length < 2) return null

  if (desktop) {
    return (
      <nav
        aria-label="Years"
        className="fixed top-20 right-1 bottom-4 z-10 flex w-12 flex-col items-center gap-0.5 overflow-y-auto [scrollbar-width:none]"
      >
        {years.map((y) => (
          <button
            key={y.year}
            type="button"
            onClick={() => onJump(y.year)}
            title={`${y.photos.toLocaleString()} photos`}
            className={cn(
              "w-full rounded-md py-1 text-xs tabular-nums text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
              current === y.year && "bg-accent font-semibold text-foreground",
            )}
          >
            {y.year}
          </button>
        ))}
      </nav>
    )
  }

  if (hidden) return null
  // A native select: the phone's own picker, and no menu code to load.
  return (
    <label className="fixed right-4 bottom-[calc(1rem+env(safe-area-inset-bottom))] z-10 flex h-11 items-center gap-2 rounded-full bg-secondary px-4 text-sm font-medium text-secondary-foreground shadow-lg tabular-nums">
      <CalendarDays className="size-4" />
      {current ?? "Years"}
      <select
        aria-label="Jump to year"
        value={current ?? ""}
        onChange={(e) => e.target.value && onJump(Number(e.target.value))}
        className="absolute inset-0 cursor-pointer opacity-0"
      >
        <option value="" disabled>
          Jump to year
        </option>
        {years.map((y) => (
          <option key={y.year} value={y.year}>
            {y.year} ({y.photos.toLocaleString()})
          </option>
        ))}
      </select>
    </label>
  )
}
