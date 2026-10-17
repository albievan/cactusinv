import { useEffect, useState } from 'react'
import { useAsync, type AsyncState } from './useAsync'

/** PAGE is how many rows a list loads at a time (the server's maximum is 200). */
export const PAGE = 200

export type Paged<T> = AsyncState<{ items: T[]; total: number }> & {
  /** more loads the next page; hasMore says whether there is one. */
  more: () => void
  hasMore: boolean
}

/**
 * usePaged loads a list a page at a time: page(offset, limit) fetches one page, and more()
 * adds the next. Changing deps (a search or filter) starts again at the first page. Lists
 * used to stop silently at the first 200 rows.
 */
export function usePaged<T>(page: (offset: number, limit: number) => Promise<{ items: T[]; total: number }>, deps: unknown[]): Paged<T> {
  const [pages, setPages] = useState(1)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => setPages(1), deps)
  const s = useAsync(async () => {
    const all = await Promise.all(Array.from({ length: pages }, (_, i) => page(i * PAGE, PAGE)))
    return { items: all.flatMap((p) => p.items), total: all[0]?.total ?? 0 }
  }, [...deps, pages])
  const hasMore = !!s.data && s.data.items.length < s.data.total
  return { ...s, more: () => setPages((p) => p + 1), hasMore }
}
