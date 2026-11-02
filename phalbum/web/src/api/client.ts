import createClient from "openapi-fetch"
import type { paths } from "./schema"

// Same origin: the Go API serves the app, and Vite proxies /api in development.
export const api = createClient<paths>({ baseUrl: "" })

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }

  static async from(res: Response): Promise<ApiError> {
    let message = res.statusText || `HTTP ${res.status}`
    try {
      const body = await res.json()
      if (typeof body?.error === "string") message = body.error
    } catch {
      // not JSON
    }
    return new ApiError(res.status, message)
  }
}

/** Returns the data of an openapi-fetch call, or throws an ApiError. */
export async function unwrap<T>(
  call: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await call
  if (!response.ok) {
    const message = (error as { error?: string } | undefined)?.error ?? response.statusText
    throw new ApiError(response.status, message || `HTTP ${response.status}`)
  }
  return data as T
}
