/**
 * allPages fetches every item of a list the API pages (at most 200 a request), for screens
 * that need the whole list, such as a select of every customer.
 */
export async function allPages<T>(page: (offset: number, limit: number) => Promise<{ items: T[]; total: number }>): Promise<T[]> {
  const out: T[] = []
  for (;;) {
    const r = await page(out.length, 200)
    out.push(...r.items)
    if (r.items.length === 0 || out.length >= r.total) return out
  }
}
