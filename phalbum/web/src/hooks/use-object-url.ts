import { useEffect, useState } from "react"

/** A blob: URL for a file, revoked when the file changes or the component unmounts. */
export function useObjectUrl(file: File | null): string | undefined {
  const [url, setUrl] = useState<string>()
  useEffect(() => {
    if (!file) return setUrl(undefined)
    const u = URL.createObjectURL(file)
    setUrl(u)
    return () => URL.revokeObjectURL(u)
  }, [file])
  return url
}
