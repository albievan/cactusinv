/** mockFetch routes `METHOD /path` to a handler; anything else is a 404 problem. */
export function mockFetch(routes: Record<string, (req: Request) => Response | Promise<Response>>) {
  vi.stubGlobal('fetch', vi.fn(async (input: Request | string) => {
    const req = input instanceof Request ? input : new Request(input)
    const path = new URL(req.url, 'http://localhost').pathname
    const handler = routes[`${req.method} ${path}`]
    return handler ? handler(req) : new Response(JSON.stringify({ type: 'x', title: 'Not found', status: 404 }), { status: 404, headers: { 'Content-Type': 'application/problem+json' } })
  }))
}

/** json answers with a JSON body. */
export const json = (body: unknown, status = 200) => () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
