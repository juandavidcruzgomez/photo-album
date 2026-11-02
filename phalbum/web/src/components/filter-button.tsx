import { lazy, Suspense, useState } from "react"
import { SlidersHorizontal } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useFilters } from "@/lib/search-params"

const FilterSheet = lazy(() => import("@/components/filter-sheet"))

/** Opens the filter sheet, which is loaded on first use. Shows how many filters apply. */
export function FilterButton() {
  const [filters] = useFilters()
  const [open, setOpen] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const active = filters.tags.length + (filters.from ? 1 : 0) + (filters.to ? 1 : 0) + (filters.sort === "asc" ? 1 : 0)

  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        className="relative shrink-0 rounded-full"
        aria-label="Filters"
        aria-haspopup="dialog"
        aria-expanded={open}
        onPointerEnter={() => void import("@/components/filter-sheet")} // warm up on hover
        onClick={() => {
          setLoaded(true)
          setOpen(true)
        }}
      >
        <SlidersHorizontal className="size-5" />
        {active > 0 && (
          <span className="absolute top-0.5 right-0.5 grid size-4 place-items-center rounded-full bg-primary text-[10px] font-medium text-primary-foreground">
            {active}
          </span>
        )}
      </Button>
      {loaded && (
        <Suspense fallback={null}>
          <FilterSheet open={open} onOpenChange={setOpen} />
        </Suspense>
      )}
    </>
  )
}
