import { Tag as TagIcon, TagsIcon, X } from "lucide-react"

import { useBulkTag } from "@/api/queries"
import type { Photo, TagRef } from "@/api/types"
import { TagPicker } from "@/components/tag-picker"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

interface SelectionBarProps {
  selected: Photo[]
  onClear: () => void
  onSelectAll: () => void
  allSelected: boolean
}

/** Actions on the selected photos: add or remove a tag on all of them. */
export default function SelectionBar({ selected, onClear, onSelectAll, allSelected }: SelectionBarProps) {
  const bulk = useBulkTag()
  const ids = selected.map((p) => p.id)

  // Tags on at least one selected photo, with how many have it.
  const present = new Map<number, { tag: TagRef; count: number }>()
  for (const p of selected) {
    for (const t of p.tags) {
      const e = present.get(t.id)
      if (e) e.count++
      else present.set(t.id, { tag: t, count: 1 })
    }
  }
  const removable = [...present.values()].sort((a, b) => a.tag.name.localeCompare(b.tag.name))

  return (
    <div className="fixed inset-x-0 bottom-0 z-20 flex justify-center p-2 pb-[calc(0.5rem+env(safe-area-inset-bottom))] md:bottom-4">
      <div
        role="toolbar"
        aria-label="Selected photos"
        className="flex w-full max-w-xl items-center gap-1 rounded-2xl border bg-popover p-1.5 text-popover-foreground shadow-xl"
      >
        <Button variant="ghost" size="icon" onClick={onClear} aria-label="Clear selection">
          <X className="size-5" />
        </Button>
        <span className="min-w-0 flex-1 truncate text-sm font-medium tabular-nums">{selected.length} selected</span>
        {!allSelected && (
          <Button variant="ghost" size="sm" onClick={onSelectAll} className="hidden sm:inline-flex">
            Select all
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" disabled={removable.length === 0 || bulk.isPending}>
              <TagsIcon /> <span className="hidden sm:inline">Remove tag</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className="max-h-[50dvh] overflow-y-auto">
            <DropdownMenuLabel>Remove from selected</DropdownMenuLabel>
            {removable.map(({ tag, count }) => (
              <DropdownMenuItem key={tag.id} onSelect={() => bulk.mutate({ photo_ids: ids, remove: [tag.id] })}>
                {tag.name}
                <span className="ml-auto pl-4 text-xs text-muted-foreground">{count}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <TagPicker align="end" onPick={(name) => bulk.mutate({ photo_ids: ids, add: [name] })}>
          <Button size="sm" disabled={bulk.isPending}>
            <TagIcon /> Add tag
          </Button>
        </TagPicker>
      </div>
    </div>
  )
}
