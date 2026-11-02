import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query"
import { toast } from "sonner"

import type { Filters } from "@/lib/search-params"
import { api, ApiError, unwrap } from "./client"
import type { PhotoDetail, PhotoList, TagRef } from "./types"

const PAGE_SIZE = 100

const fileKey = (f: File) => `${f.name}:${f.size}:${f.lastModified}`

export function photosKey(f: Filters, image: File | null) {
  const { q, similar, tags, from, to, sort } = f
  return ["photos", { q, similar, tags, from, to, sort }, image && fileKey(image)] as const
}

async function fetchPhotos(f: Filters, image: File | null, offset: number, signal: AbortSignal): Promise<PhotoList> {
  const query = {
    q: f.q || undefined,
    similar_to: f.similar || undefined,
    tags: f.tags.length ? f.tags.join(",") : undefined,
    from: f.from || undefined,
    to: f.to || undefined,
    sort: f.sort === "asc" ? ("asc" as const) : undefined,
    limit: PAGE_SIZE,
    offset,
  }
  if (!image) {
    return unwrap(api.GET("/api/photos", { params: { query }, signal }))
  }
  const body = new FormData()
  body.set("image", image)
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined) body.set(k, String(v))
  }
  const res = await fetch("/api/photos/search", { method: "POST", body, signal })
  if (!res.ok) throw await ApiError.from(res)
  return res.json()
}

/** Pages of photos for the current filters (or uploaded picture). */
export function usePhotos(f: Filters, image: File | null) {
  return useInfiniteQuery({
    queryKey: photosKey(f, image),
    queryFn: ({ pageParam, signal }) => fetchPhotos(f, image, pageParam, signal),
    initialPageParam: 0,
    getNextPageParam: (last) => last.next_offset ?? undefined,
    placeholderData: keepPreviousData,
  })
}

export function usePhoto(id: string | undefined) {
  return useQuery({
    queryKey: ["photo", id],
    queryFn: ({ signal }) => unwrap(api.GET("/api/photos/{id}", { params: { path: { id: id! } }, signal })),
    enabled: !!id,
  })
}

export function useTags() {
  return useQuery({
    queryKey: ["tags"],
    queryFn: async ({ signal }) => (await unwrap(api.GET("/api/tags", { signal }))).tags,
    staleTime: 5 * 60_000,
  })
}

/** Photo counts per year; only the tag filter applies, so every year stays reachable. */
export function useYears(tags: string[]) {
  return useQuery({
    queryKey: ["years", tags],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.GET("/api/years", {
            params: { query: { tags: tags.length ? tags.join(",") : undefined } },
            signal,
          }),
        )
      ).years,
    staleTime: 5 * 60_000,
  })
}

// --- Tag mutations ----------------------------------------------------------

type Snapshot = [readonly unknown[], unknown][]

/** Applies fn to a photo's tags everywhere the photo is cached. */
function patchPhoto(qc: QueryClient, id: string, fn: (tags: TagRef[]) => TagRef[]) {
  qc.setQueriesData<InfiniteData<PhotoList>>({ queryKey: ["photos"] }, (data) => {
    if (!data) return data
    return {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        photos: page.photos.map((p) => (p.id === id ? { ...p, tags: fn(p.tags) } : p)),
      })),
    }
  })
  qc.setQueryData<PhotoDetail>(["photo", id], (p) => p && { ...p, tags: fn(p.tags) })
}

/** Stops in-flight fetches that would overwrite an optimistic update, and snapshots the caches. */
async function cancelPhotoQueries(qc: QueryClient, id: string): Promise<Snapshot> {
  await Promise.all([qc.cancelQueries({ queryKey: ["photos"] }), qc.cancelQueries({ queryKey: ["photo", id] })])
  return [...qc.getQueriesData({ queryKey: ["photos"] }), ...qc.getQueriesData({ queryKey: ["photo", id] })]
}

function restore(qc: QueryClient, snapshot: Snapshot | undefined) {
  snapshot?.forEach(([key, data]) => qc.setQueryData(key, data))
}

/** Marks lists stale without refetching every loaded page right away. */
function settleTags(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: ["tags"] })
  qc.invalidateQueries({ queryKey: ["years"] })
  qc.invalidateQueries({ queryKey: ["photos"], refetchType: "none" })
}

const byName = (a: TagRef, b: TagRef) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" })

export function useAddTag(photoId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) =>
      unwrap(api.POST("/api/photos/{id}/tags", { params: { path: { id: photoId } }, body: { name } })),
    onMutate: async (name) => {
      const snapshot = await cancelPhotoQueries(qc, photoId)
      const lower = name.toLowerCase()
      patchPhoto(qc, photoId, (tags) =>
        tags.some((t) => t.name.toLowerCase() === lower)
          ? tags
          : [...tags, { id: -1, name, source: "user" as const }].sort(byName),
      )
      return { snapshot }
    },
    onSuccess: (tag) => {
      patchPhoto(qc, photoId, (tags) => tags.map((t) => (t.id === -1 ? tag : t)))
    },
    onError: (err, _name, ctx) => {
      restore(qc, ctx?.snapshot)
      toast.error(`Couldn't add tag: ${err.message}`)
    },
    onSettled: () => settleTags(qc),
  })
}

export function useRemoveTag(photoId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tagId: number) =>
      unwrap(api.DELETE("/api/photos/{id}/tags/{tagID}", { params: { path: { id: photoId, tagID: tagId } } })),
    onMutate: async (tagId) => {
      const snapshot = await cancelPhotoQueries(qc, photoId)
      patchPhoto(qc, photoId, (tags) => tags.filter((t) => t.id !== tagId))
      return { snapshot }
    },
    onError: (err, _id, ctx) => {
      restore(qc, ctx?.snapshot)
      toast.error(`Couldn't remove tag: ${err.message}`)
    },
    onSettled: () => settleTags(qc),
  })
}

export function useBulkTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { photo_ids: string[]; add?: string[]; remove?: number[] }) =>
      unwrap(api.POST("/api/photos/tags", { body })),
    onSuccess: (_data, { photo_ids, add }) => {
      const n = photo_ids.length
      const what = add?.length ? `Tagged ${n} photo${n === 1 ? "" : "s"}` : `Updated ${n} photo${n === 1 ? "" : "s"}`
      toast.success(what)
      qc.invalidateQueries({ queryKey: ["tags"] })
      qc.invalidateQueries({ queryKey: ["years"] })
      qc.invalidateQueries({ queryKey: ["photos"] }) // tag ids for new names are only known after a refetch
      photo_ids.forEach((id) => qc.invalidateQueries({ queryKey: ["photo", id] }))
    },
    onError: (err) => toast.error(`Couldn't update tags: ${err.message}`),
  })
}

export function useRenameTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) =>
      unwrap(api.PATCH("/api/tags/{id}", { params: { path: { id } }, body: { name } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["tags"] })
      qc.invalidateQueries({ queryKey: ["photos"] })
      qc.invalidateQueries({ queryKey: ["photo"] })
    },
  })
}

export function useDeleteTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/api/tags/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["tags"] })
      qc.invalidateQueries({ queryKey: ["years"] })
      qc.invalidateQueries({ queryKey: ["photos"] })
      qc.invalidateQueries({ queryKey: ["photo"] })
    },
    onError: (err) => toast.error(`Couldn't delete tag: ${err.message}`),
  })
}
