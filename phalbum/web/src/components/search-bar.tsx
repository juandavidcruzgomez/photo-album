import { useEffect, useRef, useState } from "react"
import { ImagePlus, Search, X } from "lucide-react"

import { useDebounced } from "@/hooks/use-debounced"
import { useObjectUrl } from "@/hooks/use-object-url"
import { useImageSearch } from "@/lib/image-search"
import { useFilters } from "@/lib/search-params"

/** Text search (debounced into ?q=) plus a button to search by a picture. */
export function SearchBar() {
  const [filters, setFilters] = useFilters()
  const { image, setImage } = useImageSearch()
  const preview = useObjectUrl(image)
  const [text, setText] = useState(filters.q)
  const debounced = useDebounced(text.trim(), 400)
  const fileInput = useRef<HTMLInputElement>(null)
  const textInput = useRef<HTMLInputElement>(null)

  // Follow the URL when the query changes elsewhere (a removed chip, back button).
  useEffect(() => setText(filters.q), [filters.q])

  useEffect(() => {
    if (debounced === filters.q) return
    // The first keystroke adds a history entry; refining the query replaces it.
    setFilters({ q: debounced }, { replace: filters.q !== "" })
  }, [debounced])

  return (
    <form
      role="search"
      className="flex h-10 min-w-0 flex-1 items-center gap-2 rounded-full border bg-background px-3 shadow-xs focus-within:ring-[3px] focus-within:ring-ring/50"
      onSubmit={(e) => {
        e.preventDefault()
        if (text.trim() !== filters.q) setFilters({ q: text.trim() })
        textInput.current?.blur() // hide the keyboard on phones
      }}
    >
      <Search className="size-4 shrink-0 text-muted-foreground" />
      {image && (
        <span className="flex shrink-0 items-center gap-1 rounded-full bg-secondary p-0.5 pr-1">
          <img src={preview} alt="Search picture" className="size-7 rounded-full object-cover" />
          <button
            type="button"
            aria-label="Stop searching by picture"
            onClick={() => setImage(null)}
            className="grid size-5 place-items-center rounded-full hover:bg-foreground/10"
          >
            <X className="size-3.5" />
          </button>
        </span>
      )}
      <input
        ref={textInput}
        type="search"
        enterKeyHint="search"
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={image ? "Add words to refine…" : "Search photos, e.g. “dog on the beach”"}
        aria-label="Search photos"
        className="min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-muted-foreground md:text-sm [&::-webkit-search-cancel-button]:hidden"
      />
      {text && (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => {
            setText("")
            setFilters({ q: "" })
          }}
          className="grid size-6 shrink-0 place-items-center rounded-full text-muted-foreground hover:bg-accent"
        >
          <X className="size-4" />
        </button>
      )}
      <button
        type="button"
        aria-label="Search by picture"
        title="Search by picture"
        onClick={() => fileInput.current?.click()}
        className="-mr-1 grid size-8 shrink-0 place-items-center rounded-full text-muted-foreground hover:bg-accent hover:text-foreground"
      >
        <ImagePlus className="size-5" />
      </button>
      <input
        ref={fileInput}
        type="file"
        accept="image/*"
        hidden
        onChange={(e) => {
          const file = e.target.files?.[0]
          e.target.value = "" // allow picking the same file again
          if (!file) return
          setImage(file)
          if (window.location.pathname !== "/") setFilters({})
          window.scrollTo(0, 0)
        }}
      />
    </form>
  )
}
