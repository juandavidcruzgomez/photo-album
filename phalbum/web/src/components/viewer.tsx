import { useMemo, useState, type ReactNode } from "react"
import Lightbox, { type Slide } from "yet-another-react-lightbox"
import Zoom from "yet-another-react-lightbox/plugins/zoom"
import "yet-another-react-lightbox/styles.css"
import { Info, Sparkles } from "lucide-react"

import { usePhoto } from "@/api/queries"
import type { Photo } from "@/api/types"
import { PhotoInfo } from "@/components/photo-info"

const THUMB_MAX = 512 // the API's thumbnail size

function toSlide(p: Photo): Slide {
  const w = p.width || 1
  const h = p.height || 1
  const scale = Math.min(1, THUMB_MAX / Math.max(w, h))
  return {
    src: p.original_url,
    alt: p.filename,
    width: w,
    height: h,
    srcSet: [
      { src: p.thumbnail_url, width: Math.round(w * scale), height: Math.round(h * scale) },
      { src: p.original_url, width: w, height: h },
    ],
  }
}

export interface ViewerProps {
  id: string
  photos: Photo[] // the loaded grid, to swipe through
  onClose: () => void
  onNavigate: (id: string) => void
  onNearEnd: () => void
  onSimilar: (id: string) => void
}

/** Full-screen photo viewer with swipe, zoom and an info panel. */
export default function Viewer({ id, photos, onClose, onNavigate, onNearEnd, onSimilar }: ViewerProps) {
  const [infoOpen, setInfoOpen] = useState(false)
  const index = photos.findIndex((p) => p.id === id)
  const inGrid = index >= 0
  // Opened from a link: show just that photo.
  const single = usePhoto(inGrid ? undefined : id)
  const list = useMemo(() => (inGrid ? photos : single.data ? [single.data] : []), [inGrid, photos, single.data])
  const slides = useMemo(() => list.map(toSlide), [list])

  if (list.length === 0) {
    return (
      <div className="fixed inset-0 z-40 grid place-items-center bg-black/90 text-white" onClick={onClose}>
        {single.isError ? "Photo not found" : "Loading…"}
      </div>
    )
  }

  const button = (label: string, icon: ReactNode, onClick: () => void) => (
    <button key={label} type="button" className="yarl__button" aria-label={label} title={label} onClick={onClick}>
      {icon}
    </button>
  )

  return (
    <>
      <Lightbox
        open
        close={onClose}
        index={Math.max(0, index)}
        slides={slides}
        plugins={[Zoom]}
        carousel={{ finite: true, preload: 2 }}
        controller={{ closeOnPullDown: true, closeOnBackdropClick: true }}
        zoom={{ maxZoomPixelRatio: 2, scrollToZoom: true }}
        animation={{ fade: 200, swipe: 250 }}
        on={{
          view: ({ index: i }) => {
            const p = list[i]
            if (p && p.id !== id) onNavigate(p.id)
            if (i >= list.length - 5) onNearEnd()
          },
        }}
        toolbar={{
          buttons: [
            button("More like this", <Sparkles className="yarl__icon" />, () => onSimilar(id)),
            button("Info and tags", <Info className="yarl__icon" />, () => setInfoOpen(true)),
            "close",
          ],
        }}
        // Below Radix popups (z-50) so the info sheet and tag picker open on top.
        styles={{ root: { "--yarl__portal_zindex": 40 } }}
      />
      <PhotoInfo
        id={id}
        open={infoOpen}
        onOpenChange={setInfoOpen}
        onSimilar={onSimilar}
        // The lightbox handles keys (Escape, arrows) only while it has focus.
        onCloseAutoFocus={(e) => {
          e.preventDefault()
          document.querySelector<HTMLElement>(".yarl__container")?.focus()
        }}
      />
    </>
  )
}
