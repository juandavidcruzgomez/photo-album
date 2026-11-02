import type { ReactNode } from "react"
import { X } from "lucide-react"

import { cn } from "@/lib/utils"

/** A rounded label with an optional remove button. */
export function Chip({
  children,
  onRemove,
  removeLabel = "Remove",
  className,
}: {
  children: ReactNode
  onRemove?: () => void
  removeLabel?: string
  className?: string
}) {
  return (
    <span
      className={cn(
        "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full bg-secondary pl-3 text-sm text-secondary-foreground",
        onRemove ? "pr-1" : "pr-3",
        className,
      )}
    >
      {children}
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={removeLabel}
          className="grid size-5 place-items-center rounded-full hover:bg-foreground/10"
        >
          <X className="size-3.5" />
        </button>
      )}
    </span>
  )
}
