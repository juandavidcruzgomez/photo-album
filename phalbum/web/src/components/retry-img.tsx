import { useEffect, useRef, useState, type ImgHTMLAttributes } from "react"
import { ImageOff } from "lucide-react"

import { cn } from "@/lib/utils"

// Waits before each retry. The server rebuilds a broken thumbnail on the first
// failed request, so a retry a moment later usually works.
const RETRY_DELAYS = [1000, 4000]

type Props = ImgHTMLAttributes<HTMLImageElement> & { src: string }

interface State {
  src: string
  attempt: number
  waiting: boolean // failed, a retry is scheduled
  failed: boolean // out of retries
}

const initial = (src: string): State => ({ src, attempt: 0, waiting: false, failed: false })

/**
 * An <img> that retries a few times when loading fails, and then shows a
 * placeholder icon instead of the browser's broken-image glyph.
 */
export function RetryImg({ src, className, onError, ...props }: Props) {
  const [state, setState] = useState(() => initial(src))
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])

  // A new src starts over.
  const s = state.src === src ? state : initial(src)
  if (s !== state) setState(s)

  if (s.failed) {
    return (
      <span role="img" aria-label={props.alt} className={cn("grid place-items-center text-muted-foreground", className)}>
        <ImageOff className="size-1/3 max-h-8 max-w-8" strokeWidth={1.5} />
      </span>
    )
  }

  const url = s.attempt === 0 ? src : `${src}${src.includes("?") ? "&" : "?"}retry=${s.attempt}`
  return (
    <img
      {...props}
      src={url}
      className={cn(className, s.waiting && "invisible")}
      onError={(e) => {
        onError?.(e)
        const delay = RETRY_DELAYS[s.attempt]
        if (delay === undefined) {
          setState({ ...s, failed: true })
          return
        }
        setState({ ...s, waiting: true })
        clearTimeout(timer.current)
        timer.current = setTimeout(() => setState((p) => (p.src === src ? { ...p, attempt: p.attempt + 1, waiting: false } : p)), delay)
      }}
    />
  )
}
