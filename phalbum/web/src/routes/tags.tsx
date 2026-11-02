import { useState } from "react"
import { Link } from "wouter"
import { Loader2, Pencil, Search, Trash2 } from "lucide-react"

import { ApiError } from "@/api/client"
import { useDeleteTag, useRenameTag, useTags } from "@/api/queries"
import type { Tag } from "@/api/types"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"

/** All tags with photo counts: open, rename or delete them. */
export default function TagsPage() {
  const { data: tags, isPending, error } = useTags()
  const [filter, setFilter] = useState("")
  const [renaming, setRenaming] = useState<Tag | null>(null)
  const [deleting, setDeleting] = useState<Tag | null>(null)
  const del = useDeleteTag()

  const shown = (tags ?? []).filter((t) => t.name.toLowerCase().includes(filter.trim().toLowerCase()))

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-4 px-4 py-4 pb-16">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold tracking-tight">Tags</h1>
        {tags && <span className="text-sm text-muted-foreground">{tags.length} tags</span>}
      </div>
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter tags…" className="pl-9" />
      </div>

      {isPending && <Loader2 className="mx-auto my-12 size-6 animate-spin text-muted-foreground" />}
      {error && <p className="text-sm text-destructive">Couldn't load tags: {error.message}</p>}
      {tags && shown.length === 0 && (
        <p className="py-12 text-center text-sm text-muted-foreground">
          {tags.length ? "No tags match." : "No tags yet. Tag photos from the viewer or by selecting several."}
        </p>
      )}

      <ul className="divide-y rounded-lg border">
        {shown.map((t) => (
          <li key={t.id} className="flex items-center gap-2 py-1 pr-1 pl-4">
            <Link href={`/?tags=${encodeURIComponent(t.name)}`} className="min-w-0 flex-1 truncate py-2 hover:underline">
              {t.name}
            </Link>
            <span className="text-sm text-muted-foreground tabular-nums">{t.photos.toLocaleString()}</span>
            <Button variant="ghost" size="icon-sm" aria-label={`Rename ${t.name}`} onClick={() => setRenaming(t)}>
              <Pencil />
            </Button>
            <Button variant="ghost" size="icon-sm" aria-label={`Delete ${t.name}`} onClick={() => setDeleting(t)}>
              <Trash2 />
            </Button>
          </li>
        ))}
      </ul>

      {renaming && <RenameDialog key={renaming.id} tag={renaming} onClose={() => setRenaming(null)} />}

      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete “{deleting?.name}”?</AlertDialogTitle>
            <AlertDialogDescription>
              The tag is removed from {deleting?.photos.toLocaleString()} photo{deleting?.photos === 1 ? "" : "s"}. The
              photos themselves are kept.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => deleting && del.mutate(deleting.id)}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function RenameDialog({ tag, onClose }: { tag: Tag; onClose: () => void }) {
  const rename = useRenameTag()
  const [name, setName] = useState(tag.name)
  const [error, setError] = useState("")

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (!name.trim() || name.trim() === tag.name) return onClose()
            rename.mutate(
              { id: tag.id, name: name.trim() },
              {
                onSuccess: onClose,
                onError: (err) =>
                  setError(err instanceof ApiError && err.status === 409 ? "Another tag already has that name." : err.message),
              },
            )
          }}
        >
          <DialogHeader>
            <DialogTitle>Rename tag</DialogTitle>
            <DialogDescription>Every photo with “{tag.name}” gets the new name.</DialogDescription>
          </DialogHeader>
          <Input value={name} onChange={(e) => setName(e.target.value)} aria-label="Tag name" autoFocus />
          {error && <p className="text-sm text-destructive">{error}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={rename.isPending}>
              Rename
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
