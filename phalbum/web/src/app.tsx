import { lazy, Suspense } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Link, Route, Switch, useLocation } from "wouter"
import { Images, Tags } from "lucide-react"

import { FilterButton } from "@/components/filter-button"
import { SearchBar } from "@/components/search-bar"
import { Button } from "@/components/ui/button"
import { Toaster } from "@/components/ui/sonner"
import { ImageSearchProvider } from "@/lib/image-search"
import Library from "@/routes/library"

const TagsPage = lazy(() => import("@/routes/tags"))
const AdminPage = lazy(() => import("@/routes/admin"))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 60_000, retry: 1, refetchOnWindowFocus: false },
  },
})

function Header() {
  const [location] = useLocation()
  const inLibrary = location === "/" || location.startsWith("/photo/")
  return (
    <header className="sticky top-0 z-30 flex h-14 items-center gap-1 bg-background/85 px-2 backdrop-blur supports-[backdrop-filter]:bg-background/70 sm:gap-2 md:px-4">
      <Link href="/" className="flex shrink-0 items-center gap-2 rounded-full p-2 font-semibold" aria-label="All photos">
        <Images className="size-5" />
        <span className="hidden sm:inline">Photos</span>
      </Link>
      <SearchBar />
      {inLibrary && <FilterButton />}
      <Button variant="ghost" size="icon" className="shrink-0 rounded-full" asChild>
        <Link href="/tags" aria-label="Tags">
          <Tags className="size-5" />
        </Link>
      </Button>
    </header>
  )
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ImageSearchProvider>
        <Header />
        <main>
          <Suspense fallback={null}>
            <Switch>
              <Route path="/tags" component={TagsPage} />
              <Route path="/admin" nest component={AdminPage} />
              <Route>
                <Library />
              </Route>
            </Switch>
          </Suspense>
        </main>
        <Toaster position="top-center" />
      </ImageSearchProvider>
    </QueryClientProvider>
  )
}
