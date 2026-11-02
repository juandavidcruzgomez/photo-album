import { Plus } from "lucide-react"

import { Chip } from "@/components/chip"
import { TagPicker } from "@/components/tag-picker"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { useIsDesktop } from "@/hooks/use-media-query"
import { useImageSearch } from "@/lib/image-search"
import { isSimilaritySearch, useFilters, type Sort } from "@/lib/search-params"
import { cn } from "@/lib/utils"

/** Tag, date and sort filters: a bottom sheet on phones, a side panel on larger screens. */
export default function FilterSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [filters, setFilters] = useFilters()
  const { image } = useImageSearch()
  const desktop = useIsDesktop()
  const active = filters.tags.length + (filters.from ? 1 : 0) + (filters.to ? 1 : 0) + (filters.sort === "asc" ? 1 : 0)
  const ranked = isSimilaritySearch(filters, !!image)

  const toggleTag = (name: string) => {
    const has = filters.tags.some((t) => t.toLowerCase() === name.toLowerCase())
    setFilters({
      tags: has ? filters.tags.filter((t) => t.toLowerCase() !== name.toLowerCase()) : [...filters.tags, name],
    })
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side={desktop ? "right" : "bottom"} className={cn(!desktop && "max-h-[85dvh] rounded-t-xl")}>
        <SheetHeader>
          <SheetTitle>Filters</SheetTitle>
          <SheetDescription className="sr-only">Filter photos by tags and dates, and choose the order.</SheetDescription>
        </SheetHeader>

        <div className="flex flex-col gap-6 overflow-y-auto px-4">
          <section className="flex flex-col gap-2">
            <Label>Tags</Label>
            <p className="text-xs text-muted-foreground">Photos must have all of these tags.</p>
            <div className="flex flex-wrap gap-1.5">
              {filters.tags.map((t) => (
                <Chip key={t} onRemove={() => toggleTag(t)} removeLabel={`Remove ${t}`}>
                  {t}
                </Chip>
              ))}
              <TagPicker onPick={toggleTag} selected={filters.tags} allowCreate={false}>
                <Button variant="outline" size="sm" className="h-7 rounded-full">
                  <Plus /> Add tag
                </Button>
              </TagPicker>
            </div>
          </section>

          <section className="flex flex-col gap-2">
            <Label>Dates</Label>
            <div className="grid grid-cols-2 gap-2">
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                From
                <Input
                  type="date"
                  value={filters.from}
                  max={filters.to || undefined}
                  onChange={(e) => setFilters({ from: e.target.value }, { replace: true })}
                />
              </label>
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                To
                <Input
                  type="date"
                  value={filters.to}
                  min={filters.from || undefined}
                  onChange={(e) => setFilters({ to: e.target.value }, { replace: true })}
                />
              </label>
            </div>
          </section>

          <section className="flex flex-col gap-2">
            <Label>Order</Label>
            <div className="grid grid-cols-2 gap-1 rounded-lg bg-muted p-1" role="radiogroup">
              {(
                [
                  ["desc", "Newest first"],
                  ["asc", "Oldest first"],
                ] as [Sort, string][]
              ).map(([value, label]) => (
                <button
                  key={value}
                  type="button"
                  role="radio"
                  aria-checked={filters.sort === value}
                  onClick={() => setFilters({ sort: value }, { replace: true })}
                  className={cn(
                    "h-8 rounded-md text-sm transition-colors",
                    filters.sort === value ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
            {ranked && <p className="text-xs text-muted-foreground">Searches are ordered by similarity.</p>}
          </section>
        </div>

        <SheetFooter>
          <Button
            variant="outline"
            disabled={active === 0}
            onClick={() => setFilters({ tags: [], from: "", to: "", sort: "desc" })}
          >
            Clear filters
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
