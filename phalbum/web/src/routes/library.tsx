import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { useLocation, useRoute, useSearch } from "wouter"
import { ImageOff, Loader2 } from "lucide-react"

import { usePhotos, useYears } from "@/api/queries"
import type { Photo } from "@/api/types"
import { ActiveFilters } from "@/components/active-filters"
import { PhotoGrid, type PhotoGridHandle } from "@/components/photo-grid"
import { Button } from "@/components/ui/button"
import { YearNav } from "@/components/year-nav"
import { useIsDesktop } from "@/hooks/use-media-query"
import { useImageSearch } from "@/lib/image-search"
import { isSimilaritySearch, useFilters } from "@/lib/search-params"
import { cn } from "@/lib/utils"

const Viewer = lazy(() => import("@/components/viewer"))
const SelectionBar = lazy(() => import("@/components/selection-bar"))

/** The photo grid, with search results, year navigation, selection and the viewer at /photo/:id. */
export default function Library() {
  const [filters, setFilters] = useFilters()
  const { image, setImage } = useImageSearch()
  const ranked = isSimilaritySearch(filters, !!image)
  const desktop = useIsDesktop()

  const query = usePhotos(filters, image)
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query
  const photos = useMemo(() => query.data?.pages.flatMap((p) => p.photos) ?? [], [query.data])
  const years = useYears(filters.tags)
  const yearCounts = useMemo(() => new Map(years.data?.map((y) => [y.year, y.photos])), [years.data])

  const [, params] = useRoute("/photo/:id")
  const [, navigate] = useLocation()
  const search = useSearch()
  const qs = search ? `?${search}` : ""

  // --- Viewer -------------------------------------------------------------
  const open = useCallback(
    (p: Photo) => navigate(`/photo/${p.id}${qs}`, { state: { fromGrid: true } }),
    [navigate, qs],
  )
  const close = useCallback(() => {
    if (history.state?.fromGrid) history.back()
    else navigate(`/${qs}`, { replace: true })
  }, [navigate, qs])
  const showPhoto = useCallback(
    (id: string) => navigate(`/photo/${id}${qs}`, { replace: true, state: history.state }),
    [navigate, qs],
  )
  const nearEnd = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) fetchNextPage()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])
  const similar = useCallback(
    (id: string) => {
      setImage(null)
      setFilters({ similar: id, q: "" })
      window.scrollTo(0, 0)
    },
    [setFilters, setImage],
  )

  // --- Selection ----------------------------------------------------------
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const anchor = useRef<number | null>(null)
  const photosRef = useRef(photos)
  photosRef.current = photos

  const toggle = useCallback((p: Photo, index: number, range: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (range && anchor.current !== null) {
        const [a, b] = [Math.min(anchor.current, index), Math.max(anchor.current, index)]
        photosRef.current.slice(a, b + 1).forEach((x) => next.add(x.id))
      } else if (next.has(p.id)) {
        next.delete(p.id)
      } else {
        next.add(p.id)
      }
      return next
    })
    anchor.current = index
  }, [])
  const clearSelection = useCallback(() => {
    setSelected(new Set())
    anchor.current = null
  }, [])

  // A new search starts with nothing selected.
  const resultsKey = JSON.stringify([filters, image?.name, image?.size])
  useEffect(clearSelection, [resultsKey, clearSelection])

  useEffect(() => {
    if (selected.size === 0) return
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && !params && clearSelection()
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [selected.size, params, clearSelection])

  const selectedPhotos = useMemo(() => photos.filter((p) => selected.has(p.id)), [photos, selected])

  // --- Years ----------------------------------------------------------------
  const grid = useRef<PhotoGridHandle>(null)
  const [visibleYear, setVisibleYear] = useState<number | null>(null)
  const jumpToYear = (year: number) => {
    if (grid.current?.scrollToYear(year)) return
    // Not loaded yet: start the list at that year.
    if (filters.sort === "asc") setFilters({ from: `${year}-01-01`, to: "" })
    else setFilters({ to: `${year}-12-31`, from: "" })
    window.scrollTo(0, 0)
  }
  const showYears = !ranked && (years.data?.length ?? 0) > 1

  return (
    <>
      <ActiveFilters />
      <div className={cn("px-0.5 pb-24 sm:px-2 md:px-4", showYears && desktop && "pr-14 md:pr-14")}>
        {query.isPending ? (
          <div className="grid place-items-center py-24 text-muted-foreground">
            <Loader2 className="size-8 animate-spin" />
          </div>
        ) : query.isError ? (
          <Empty title="Couldn't load photos" detail={query.error.message}>
            <Button variant="outline" onClick={() => query.refetch()}>
              Try again
            </Button>
          </Empty>
        ) : photos.length === 0 ? (
          <Empty title="No photos" detail={ranked || filters.tags.length || filters.from || filters.to ? "Nothing matches these filters." : "The library is empty. Run the crawler and indexer to add photos."} />
        ) : (
          <div className={cn("transition-opacity", query.isPlaceholderData && "opacity-60")}>
            <PhotoGrid
              ref={grid}
              photos={photos}
              grouped={!ranked}
              yearCounts={yearCounts}
              hasNextPage={hasNextPage}
              isFetchingNextPage={isFetchingNextPage}
              fetchNextPage={fetchNextPage}
              selected={selected}
              onOpen={open}
              onToggle={toggle}
              onVisibleYear={setVisibleYear}
            />
          </div>
        )}
      </div>

      {showYears && (
        <YearNav years={years.data!} current={visibleYear} onJump={jumpToYear} hidden={selected.size > 0} />
      )}

      {selected.size > 0 && (
        <Suspense fallback={null}>
          <SelectionBar
            selected={selectedPhotos}
            onClear={clearSelection}
            allSelected={selected.size === photos.length}
            onSelectAll={() => setSelected(new Set(photos.map((p) => p.id)))}
          />
        </Suspense>
      )}

      {params && (
        <Suspense fallback={null}>
          <Viewer
            id={params.id}
            photos={photos}
            onClose={close}
            onNavigate={showPhoto}
            onNearEnd={nearEnd}
            onSimilar={similar}
          />
        </Suspense>
      )}
    </>
  )
}

function Empty({ title, detail, children }: { title: string; detail?: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-3 py-24 text-center">
      <ImageOff className="size-10 text-muted-foreground" />
      <h2 className="text-lg font-medium">{title}</h2>
      {detail && <p className="max-w-sm text-sm text-muted-foreground">{detail}</p>}
      {children}
    </div>
  )
}
