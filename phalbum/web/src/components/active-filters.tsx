import { Chip } from "@/components/chip"
import { formatDay } from "@/lib/format"
import { useFilters } from "@/lib/search-params"

/** The applied filters as removable chips. */
export function ActiveFilters() {
  const [f, setFilters] = useFilters()
  const empty = !f.similar && !f.tags.length && !f.from && !f.to && f.sort === "desc"
  if (empty) return null

  return (
    <div className="flex gap-1.5 overflow-x-auto px-2 pb-2 [scrollbar-width:none] md:flex-wrap md:px-4">
      {f.similar && (
        <Chip onRemove={() => setFilters({ similar: "" })} removeLabel="Stop showing similar photos" className="pl-0.5">
          <img src={`/api/photos/${f.similar}/thumbnail`} alt="" className="size-6 rounded-full object-cover" />
          Similar photos
        </Chip>
      )}
      {f.tags.map((t) => (
        <Chip key={t} onRemove={() => setFilters({ tags: f.tags.filter((x) => x !== t) })} removeLabel={`Remove ${t}`}>
          #{t}
        </Chip>
      ))}
      {f.from && (
        <Chip onRemove={() => setFilters({ from: "" })} removeLabel="Remove start date">
          From {formatDay(f.from)}
        </Chip>
      )}
      {f.to && (
        <Chip onRemove={() => setFilters({ to: "" })} removeLabel="Remove end date">
          Until {formatDay(f.to)}
        </Chip>
      )}
      {f.sort === "asc" && (
        <Chip onRemove={() => setFilters({ sort: "desc" })} removeLabel="Show newest first">
          Oldest first
        </Chip>
      )}
    </div>
  )
}
