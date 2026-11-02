import { createContext, useContext, useState, type ReactNode } from "react"

// The picture being searched with. A File can't live in the URL, so it is
// kept here and lost on reload.
const ImageSearchContext = createContext<{ image: File | null; setImage: (f: File | null) => void }>({
  image: null,
  setImage: () => {},
})

export function ImageSearchProvider({ children }: { children: ReactNode }) {
  const [image, setImage] = useState<File | null>(null)
  return <ImageSearchContext value={{ image, setImage }}>{children}</ImageSearchContext>
}

export const useImageSearch = () => useContext(ImageSearchContext)
