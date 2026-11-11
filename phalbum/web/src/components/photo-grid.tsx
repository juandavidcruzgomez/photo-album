import { forwardRef, memo, useEffect, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react"
import { useWindowVirtualizer } from "@tanstack/react-virtual"
import { computeRowsLayout } from "react-photo-album"
import { Check, Loader2 } from "lucide-react"

import type { Photo } from "@/api/types"
import { RetryImg } from "@/components/retry-img"
import { useLongPress } from "@/hooks/use-long-press"
import { yearOf } from "@/lib/format"
import { cn } from "@/lib/utils"

const HEADER_HEIGHT = 52
const STICKY_OFFSET = 64 // the app header, plus a little air

interface Tile {
  photo: Photo
  index: number // in the flat photo list
  width: number
  height: number
}

type Item =
  | { kind: "header"; key: string; year: number | null; size: number }
  | { kind: "row"; key: string; year: number | null; tiles: Tile[]; size: number }

export interface PhotoGridHandle {
  /** Scrolls to a year's header; false when that year isn't loaded. */
  scrollToYear(year: number): boolean
}

interface PhotoGridProps {
  photos: Photo[]
  grouped: boolean // year headers (date order) or a flat list (similarity order)
  yearCounts?: Map<number, number>
  hasNextPage: boolean
  isFetchingNextPage: boolean
  fetchNextPage: () => void
  selected: Set<string>
  onOpen: (photo: Photo) => void
  onToggle: (photo: Photo, index: number, range: boolean) => void
  onVisibleYear?: (year: number | null) => void
}

function rowHeightFor(width: number) {
  return width < 640 ? 120 : width < 1024 ? 180 : 220
}

/** Justified rows, split into year sections, laid out for the container width. */
function layout(photos: Photo[], grouped: boolean, width: number): Item[] {
  if (width <= 0 || photos.length === 0) return []
  const target = rowHeightFor(width)
  const spacing = width < 640 ? 2 : 4

  const sections: { year: number | null; start: number; photos: Photo[] }[] = []
  photos.forEach((p, i) => {
    const year = grouped ? yearOf(p.captured_at) : null
    const last = sections[sections.length - 1]
    if (last && last.year === year) last.photos.push(p)
    else sections.push({ year, start: i, photos: [p] })
  })

  const items: Item[] = []
  for (const s of sections) {
    if (grouped) items.push({ kind: "header", key: `h-${s.year}-${s.start}`, year: s.year, size: HEADER_HEIGHT })
    const input = s.photos.map((photo, i) => ({
      src: photo.thumbnail_url,
      width: photo.width || 1,
      height: photo.height || 1,
      photo,
      index: s.start + i,
    }))
    const model = computeRowsLayout(input, spacing, 0, width, target)
    const tracks = model
      ? model.tracks.map((t) => t.photos.map(({ photo, width, height }) => ({ photo: photo.photo, index: photo.index, width, height })))
      : input.map((p) => [{ photo: p.photo, index: p.index, width: Math.min(width, (target * p.width) / p.height), height: target }])

    // A short last row would be stretched to the full width; keep it at the target height instead.
    const last = tracks[tracks.length - 1]
    if (last && last[0].height > target * 1.3) {
      tracks[tracks.length - 1] = last.map((t) => ({ ...t, width: (target * t.width) / t.height, height: target }))
    }

    for (const tiles of tracks) {
      items.push({ kind: "row", key: tiles[0].photo.id, year: s.year, tiles, size: tiles[0].height + spacing })
    }
  }
  return items
}

export const PhotoGrid = forwardRef<PhotoGridHandle, PhotoGridProps>(function PhotoGrid(
  { photos, grouped, yearCounts, hasNextPage, isFetchingNextPage, fetchNextPage, selected, onOpen, onToggle, onVisibleYear },
  ref,
) {
  const container = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [scrollMargin, setScrollMargin] = useState(0)

  useLayoutEffect(() => {
    const el = container.current
    if (!el) return
    const observer = new ResizeObserver(() => {
      setWidth(el.clientWidth)
      setScrollMargin(el.getBoundingClientRect().top + window.scrollY)
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const items = useMemo(() => layout(photos, grouped, width), [photos, grouped, width])
  const selecting = selected.size > 0

  const virtualizer = useWindowVirtualizer({
    count: items.length + (hasNextPage ? 1 : 0),
    estimateSize: (i) => items[i]?.size ?? 96,
    getItemKey: (i) => items[i]?.key ?? "loader",
    overscan: 3,
    scrollMargin,
    scrollPaddingStart: STICKY_OFFSET,
  })

  // Sizes are computed, not measured: drop cached sizes when the layout changes.
  useEffect(() => virtualizer.measure(), [items, virtualizer])

  const virtualItems = virtualizer.getVirtualItems()
  const lastIndex = virtualItems[virtualItems.length - 1]?.index ?? -1
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && lastIndex >= items.length - 4) fetchNextPage()
  }, [lastIndex, items.length, hasNextPage, isFetchingNextPage, fetchNextPage])

  // The year at the top of the viewport.
  const offset = (virtualizer.scrollOffset ?? 0) + STICKY_OFFSET
  const topItem = virtualItems.find((v) => v.end > offset)
  const visibleYear = topItem ? (items[topItem.index]?.year ?? null) : null
  useEffect(() => onVisibleYear?.(visibleYear), [visibleYear, onVisibleYear])

  useImperativeHandle(
    ref,
    () => ({
      scrollToYear(year) {
        const i = items.findIndex((it) => it.kind === "header" && it.year === year)
        if (i < 0) return false
        virtualizer.scrollToIndex(i, { align: "start" })
        return true
      },
    }),
    [items, virtualizer],
  )

  return (
    <div ref={container} className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
      {virtualItems.map((v) => {
        const item = items[v.index]
        const style = {
          position: "absolute" as const,
          top: 0,
          left: 0,
          width: "100%",
          height: v.size,
          transform: `translateY(${v.start - scrollMargin}px)`,
        }
        if (!item) {
          return (
            <div key={v.key} style={style} className="grid place-items-center text-muted-foreground">
              {isFetchingNextPage && <Loader2 className="size-6 animate-spin" />}
            </div>
          )
        }
        if (item.kind === "header") {
          const count = item.year !== null ? yearCounts?.get(item.year) : undefined
          return (
            <div key={v.key} style={style} className="flex items-end gap-2 px-1 pb-2">
              <h2 className="text-xl font-semibold tracking-tight">{item.year ?? "No date"}</h2>
              {count !== undefined && (
                <span className="pb-0.5 text-sm text-muted-foreground">
                  {count.toLocaleString()} photo{count === 1 ? "" : "s"}
                </span>
              )}
            </div>
          )
        }
        return (
          <div key={v.key} style={style} className="flex gap-0.5 sm:gap-1">
            {item.tiles.map((t) => (
              <GridTile
                key={t.photo.id}
                tile={t}
                selected={selected.has(t.photo.id)}
                selecting={selecting}
                onOpen={onOpen}
                onToggle={onToggle}
              />
            ))}
          </div>
        )
      })}
    </div>
  )
})

interface GridTileProps {
  tile: Tile
  selected: boolean
  selecting: boolean
  onOpen: (photo: Photo) => void
  onToggle: (photo: Photo, index: number, range: boolean) => void
}

const GridTile = memo(function GridTile({ tile, selected, selecting, onOpen, onToggle }: GridTileProps) {
  const { photo, index, width, height } = tile
  const longPress = useLongPress(() => onToggle(photo, index, false))

  return (
    <div
      className={cn("group relative shrink-0 overflow-hidden bg-muted", selected && "bg-selection/25")}
      style={{ width, height }}
    >
      <a
        href={`/photo/${photo.id}`}
        draggable={false}
        onClick={(e) => {
          if (e.metaKey || e.ctrlKey || e.button !== 0) return // let the browser open a new tab
          e.preventDefault()
          if (longPress.wasFired()) return
          if (selecting || e.shiftKey) onToggle(photo, index, e.shiftKey)
          else onOpen(photo)
        }}
        {...longPress.handlers}
        className="block size-full focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
      >
        <RetryImg
          src={photo.thumbnail_url}
          alt={photo.filename}
          loading="lazy"
          decoding="async"
          draggable={false}
          className={cn(
            "size-full object-cover transition-transform duration-150 select-none [-webkit-touch-callout:none]",
            selected && "scale-[0.88] rounded-md",
          )}
        />
      </a>
      <button
        type="button"
        aria-label={selected ? "Deselect" : "Select"}
        aria-pressed={selected}
        onClick={(e) => onToggle(photo, index, e.shiftKey)}
        className={cn(
          "absolute top-1.5 left-1.5 grid size-6 place-items-center rounded-full border-2 border-white/90 bg-black/25 text-white shadow transition-opacity",
          "opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
          (selecting || selected) && "opacity-100",
          selected && "border-selection bg-selection",
        )}
      >
        {selected && <Check className="size-4" strokeWidth={3} />}
      </button>
    </div>
  )
})
