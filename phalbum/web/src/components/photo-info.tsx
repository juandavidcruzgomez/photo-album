import type { ReactNode } from "react"
import { ExternalLink, Sparkles } from "lucide-react"

import { usePhoto } from "@/api/queries"
import { TagEditor } from "@/components/tag-editor"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useIsDesktop } from "@/hooks/use-media-query"
import { formatBytes, formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"

interface PhotoInfoProps {
  id: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onSimilar: (id: string) => void
  /** Where focus goes when the sheet closes (by default, its trigger). */
  onCloseAutoFocus?: (e: Event) => void
}

/** Details, tags and actions for one photo. */
export function PhotoInfo({ id, open, onOpenChange, onSimilar, onCloseAutoFocus }: PhotoInfoProps) {
  const desktop = useIsDesktop()
  const { data: p, isLoading, error } = usePhoto(id)

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side={desktop ? "right" : "bottom"}
        onCloseAutoFocus={onCloseAutoFocus}
        className={cn("gap-0", desktop ? "w-96 sm:max-w-md" : "max-h-[75dvh] rounded-t-xl")}
      >
        <SheetHeader>
          <SheetTitle className="truncate pr-6">{p?.filename ?? "Photo"}</SheetTitle>
          <SheetDescription>{p ? formatDateTime(p.captured_at) : isLoading ? "Loading…" : ""}</SheetDescription>
        </SheetHeader>

        {error && <p className="px-4 text-sm text-destructive">Couldn't load details: {error.message}</p>}
        {p && (
          <div className="flex flex-col gap-6 overflow-y-auto px-4 pb-6">
            <div className="flex gap-2">
              <Button size="sm" onClick={() => onSimilar(p.id)}>
                <Sparkles /> More like this
              </Button>
              <Button size="sm" variant="outline" asChild>
                <a href={p.original_url} target="_blank" rel="noreferrer">
                  <ExternalLink /> Original
                </a>
              </Button>
            </div>

            <section className="flex flex-col gap-2">
              <h3 className="text-sm font-medium">Tags</h3>
              <TagEditor photoId={p.id} tags={p.tags} />
            </section>

            <section className="flex flex-col gap-2">
              <h3 className="text-sm font-medium">Details</h3>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
                <Row label="Size">
                  {p.width} × {p.height} · {formatBytes(p.size)} · {p.extension.replace(/^\./, "").toUpperCase()}
                </Row>
                {(p.camera_make || p.camera_model) && (
                  <Row label="Camera">{[p.camera_make, p.camera_model].filter(Boolean).join(" ")}</Row>
                )}
                <Row label="Taken">{p.taken_at ? formatDateTime(p.taken_at) : "Not in EXIF"}</Row>
                {p.latitude != null && p.longitude != null && (
                  <Row label="Location">
                    <a
                      className="underline underline-offset-2"
                      target="_blank"
                      rel="noreferrer"
                      href={`https://www.openstreetmap.org/?mlat=${p.latitude}&mlon=${p.longitude}#map=15/${p.latitude}/${p.longitude}`}
                    >
                      {p.latitude.toFixed(5)}, {p.longitude.toFixed(5)}
                    </a>
                  </Row>
                )}
                <Row label="Indexed">{formatDateTime(p.indexed_at)}</Row>
              </dl>
            </section>

            {p.paths.length > 0 && (
              <section className="flex flex-col gap-2">
                <h3 className="text-sm font-medium">Found at</h3>
                <ul className="flex flex-col gap-1 font-mono text-xs break-all text-muted-foreground">
                  {p.paths.map((path) => (
                    <li key={path}>{path}</li>
                  ))}
                </ul>
              </section>
            )}

            {p.exif && Object.keys(p.exif).length > 0 && (
              <details className="text-sm">
                <summary className="cursor-pointer font-medium">EXIF</summary>
                <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
                  {Object.entries(p.exif)
                    .sort(([a], [b]) => a.localeCompare(b))
                    .map(([k, v]) => (
                      <Row key={k} label={k}>
                        {v}
                      </Row>
                    ))}
                </dl>
              </details>
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  )
}
