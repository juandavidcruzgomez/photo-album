import { useCallback } from "react"
import { useLocation, useSearch } from "wouter"

export type Sort = "desc" | "asc"

/** What the library shows; it all lives in the URL query string. */
export interface Filters {
  q: string
  similar: string // photo id for "more like this"
  tags: string[]
  from: string // YYYY-MM-DD
  to: string
  sort: Sort
}

export function parseFilters(search: string): Filters {
  const p = new URLSearchParams(search)
  return {
    q: p.get("q") ?? "",
    similar: p.get("similar_to") ?? "",
    tags: (p.get("tags") ?? "")
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean),
    from: p.get("from") ?? "",
    to: p.get("to") ?? "",
    sort: p.get("sort") === "asc" ? "asc" : "desc",
  }
}

/** Returns "" or "?…" for the non-default filters. */
export function filtersToSearch(f: Filters): string {
  const p = new URLSearchParams()
  if (f.q) p.set("q", f.q)
  if (f.similar) p.set("similar_to", f.similar)
  if (f.tags.length) p.set("tags", f.tags.join(","))
  if (f.from) p.set("from", f.from)
  if (f.to) p.set("to", f.to)
  if (f.sort === "asc") p.set("sort", "asc")
  const s = p.toString()
  return s ? `?${s}` : ""
}

/** Whether results are ranked by similarity rather than listed by date. */
export function isSimilaritySearch(f: Filters, hasImage: boolean): boolean {
  return hasImage || f.q !== "" || f.similar !== ""
}

/**
 * The current filters, and a function that changes some of them. Changing
 * filters always goes back to the library ("/"), closing the photo viewer.
 */
export function useFilters(): [Filters, (patch: Partial<Filters>, opts?: { replace?: boolean }) => void] {
  const search = useSearch()
  const [, navigate] = useLocation()
  const filters = parseFilters(search)
  const update = useCallback(
    (patch: Partial<Filters>, opts?: { replace?: boolean }) => {
      const next = { ...parseFilters(window.location.search), ...patch }
      navigate(`/${filtersToSearch(next)}`, { replace: opts?.replace })
    },
    [navigate],
  )
  return [filters, update]
}
