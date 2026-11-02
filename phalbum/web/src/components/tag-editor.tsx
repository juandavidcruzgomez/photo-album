import { Folder, Plus, Tag as TagIcon } from "lucide-react"
import { Link } from "wouter"

import { useAddTag, useRemoveTag } from "@/api/queries"
import type { TagRef } from "@/api/types"
import { Chip } from "@/components/chip"
import { TagPicker } from "@/components/tag-picker"
import { Button } from "@/components/ui/button"

/** A photo's tags, with remove buttons and a picker to add more. */
export function TagEditor({ photoId, tags }: { photoId: string; tags: TagRef[] }) {
  const add = useAddTag(photoId)
  const remove = useRemoveTag(photoId)

  return (
    <div className="flex flex-wrap gap-1.5">
      {tags.map((t) => (
        <Chip
          key={`${t.id}-${t.name}`}
          onRemove={t.id > 0 ? () => remove.mutate(t.id) : undefined}
          removeLabel={`Remove tag ${t.name}`}
        >
          {t.source === "path" ? (
            <Folder className="size-3.5 text-muted-foreground" aria-label="From folder name" />
          ) : (
            <TagIcon className="size-3.5 text-muted-foreground" />
          )}
          <Link href={`/?tags=${encodeURIComponent(t.name)}`} className="hover:underline">
            {t.name}
          </Link>
        </Chip>
      ))}
      <TagPicker onPick={(name) => add.mutate(name)} exclude={tags.map((t) => t.name)}>
        <Button variant="outline" size="sm" className="h-7 rounded-full">
          <Plus /> Add tag
        </Button>
      </TagPicker>
    </div>
  )
}
