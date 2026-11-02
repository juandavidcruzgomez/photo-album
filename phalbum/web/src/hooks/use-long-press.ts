import { useRef, type PointerEvent } from "react"

/**
 * Long-press on touch screens (mouse presses are ignored). Call wasFired() in
 * the click handler to swallow the click that ends a long press.
 */
export function useLongPress(onLongPress: () => void, ms = 450) {
  const timer = useRef<number | undefined>(undefined)
  const start = useRef<{ x: number; y: number } | null>(null)
  const fired = useRef(false)
  const touch = useRef(false)

  const cancel = () => {
    window.clearTimeout(timer.current)
    start.current = null
  }

  return {
    wasFired() {
      const f = fired.current
      fired.current = false
      return f
    },
    handlers: {
      onPointerDown(e: PointerEvent) {
        touch.current = e.pointerType !== "mouse"
        if (!touch.current) return
        fired.current = false
        start.current = { x: e.clientX, y: e.clientY }
        timer.current = window.setTimeout(() => {
          fired.current = true
          navigator.vibrate?.(10)
          onLongPress()
        }, ms)
      },
      onPointerMove(e: PointerEvent) {
        if (start.current && Math.hypot(e.clientX - start.current.x, e.clientY - start.current.y) > 10) cancel()
      },
      onPointerUp: cancel,
      onPointerCancel: cancel,
      onPointerLeave: cancel,
      onContextMenu(e: Event | { preventDefault(): void }) {
        if (touch.current) e.preventDefault() // no "open link" menu on Android
      },
    },
  }
}
