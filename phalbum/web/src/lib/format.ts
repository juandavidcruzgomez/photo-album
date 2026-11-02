const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" })
const dateTimeFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" })

export function formatDate(iso: string | null | undefined): string {
  return iso ? dateFormat.format(new Date(iso)) : "Unknown date"
}

export function formatDateTime(iso: string | null | undefined): string {
  return iso ? dateTimeFormat.format(new Date(iso)) : "Unknown date"
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ["KB", "MB", "GB"]
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

/**
 * The capture year, or null for undated photos. Read from the string, not
 * converted to local time: the API writes dates in the same zone it uses to
 * count photos per year.
 */
export function yearOf(iso: string | null | undefined): number | null {
  const y = iso ? parseInt(iso.slice(0, 4), 10) : NaN
  return isNaN(y) ? null : y
}

const dayFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeZone: "UTC" })

/** Formats a bare YYYY-MM-DD date without shifting it to the local time zone. */
export function formatDay(day: string): string {
  const d = new Date(`${day}T00:00:00Z`)
  return isNaN(d.getTime()) ? day : dayFormat.format(d)
}
