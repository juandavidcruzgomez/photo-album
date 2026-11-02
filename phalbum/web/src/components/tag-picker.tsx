import { useState, type ReactNode } from "react"
import { Check, Plus } from "lucide-react"

import { useTags } from "@/api/queries"
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"

interface TagPickerProps {
  children: ReactNode // the trigger
  onPick: (name: string) => void
  /** Names shown with a check mark; the picker stays open to pick several. */
  selected?: string[]
  /** Names left out of the list. */
  exclude?: string[]
  allowCreate?: boolean
  align?: "start" | "center" | "end"
}

/** A searchable tag list in a popover, with "Create …" for new names. */
export function TagPicker({ children, onPick, selected, exclude = [], allowCreate = true, align = "start" }: TagPickerProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const { data: tags = [] } = useTags()

  const multi = selected !== undefined
  const lower = (s: string) => s.toLowerCase()
  const checked = new Set((selected ?? []).map(lower))
  const excluded = new Set(exclude.map(lower))
  const name = search.trim()
  const exists = tags.some((t) => lower(t.name) === lower(name))

  const pick = (n: string) => {
    onPick(n)
    setSearch("")
    if (!multi) setOpen(false)
  }

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (!o) setSearch("")
      }}
    >
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent className="w-64 p-0" align={align}>
        <Command>
          <CommandInput
            value={search}
            onValueChange={setSearch}
            placeholder={allowCreate ? "Find or create a tag…" : "Find a tag…"}
          />
          <CommandList>
            <CommandEmpty>{allowCreate ? "Type a name to create a tag" : "No tags found"}</CommandEmpty>
            {allowCreate && name && !exists && (
              <CommandGroup>
                <CommandItem value={`create ${name}`} onSelect={() => pick(name)}>
                  <Plus />
                  Create “{name}”
                </CommandItem>
              </CommandGroup>
            )}
            <CommandGroup>
              {tags
                .filter((t) => !excluded.has(lower(t.name)))
                .map((t) => (
                  <CommandItem key={t.id} value={t.name} onSelect={() => pick(t.name)}>
                    {multi && <Check className={cn(checked.has(lower(t.name)) ? "opacity-100" : "opacity-0")} />}
                    <span className="truncate">{t.name}</span>
                    <span className="ml-auto text-xs text-muted-foreground">{t.photos}</span>
                  </CommandItem>
                ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
