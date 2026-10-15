import { useMemo, useState } from 'react'

export interface Series {
  key: string
  label: string
  color: string // validated categorical slot (see Dashboard)
  values: number[] // one per category, same order
}

/** Clean axis ticks: 0, step, 2·step… with step a 1/2/2.5/5 × 10^n. */
function niceTicks(max: number, target = 4): number[] {
  if (max <= 0) return [0, 1]
  const raw = max / target
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? 10 * mag
  const top = Math.ceil(max / step) * step
  return Array.from({ length: Math.round(top / step) + 1 }, (_, i) => i * step)
}

const fmt = (n: number) => n.toLocaleString(undefined, { maximumFractionDigits: 0 })
const fmt2 = (n: number) => n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })

/**
 * ColumnChart draws grouped columns (one group per category) on a single y-axis, with
 * a legend, hairline gridlines, per-column hover tooltips and an accessible table view.
 */
export function ColumnChart({ categories, series, unit, height = 240 }: { categories: string[]; series: Series[]; unit: string; height?: number }) {
  const [hover, setHover] = useState<{ c: number; s: number } | null>(null)
  const [table, setTable] = useState(false)
  const max = Math.max(0, ...series.flatMap((s) => s.values))
  const ticks = useMemo(() => niceTicks(max), [max])
  const top = ticks[ticks.length - 1] || 1
  const W = 720
  const padL = 56, padR = 8, padT = 12, padB = 28
  const plotW = W - padL - padR
  const plotH = height - padT - padB
  const band = plotW / Math.max(categories.length, 1)
  const gap = 2
  const barW = Math.min(24, (band * 0.7 - gap * (series.length - 1)) / series.length)
  const groupW = barW * series.length + gap * (series.length - 1)
  const y = (v: number) => padT + plotH - (v / top) * plotH
  const r = 4

  return (
    <div>
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <ul className="flex flex-wrap gap-4 text-sm text-slate-700" aria-label="Legend">
          {series.map((s) => (
            <li key={s.key} className="flex items-center gap-2">
              <span className="inline-block h-3 w-3 rounded-sm" style={{ background: s.color }} aria-hidden="true" />
              {s.label}
            </li>
          ))}
        </ul>
        <button type="button" className="text-xs font-semibold text-brand-700 hover:underline" onClick={() => setTable((t) => !t)} aria-pressed={table}>
          {table ? 'Show chart' : 'Show table'}
        </button>
      </div>
      {table ? (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="table-head">
              <tr>
                <th className="px-3 py-2">Month</th>
                {series.map((s) => <th key={s.key} className="px-3 py-2 text-right">{s.label} ({unit})</th>)}
              </tr>
            </thead>
            <tbody className="divide-y divide-cream-200">
              {categories.map((c, i) => (
                <tr key={c}>
                  <td className="px-3 py-1.5">{c}</td>
                  {series.map((s) => <td key={s.key} className="px-3 py-1.5 text-right tabular-nums">{fmt2(s.values[i] ?? 0)}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="relative">
          <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="img" aria-label={`${series.map((s) => s.label).join(' and ')} per month, ${unit}`}>
            {ticks.map((t) => (
              <g key={t}>
                <line x1={padL} x2={W - padR} y1={y(t)} y2={y(t)} stroke="#e7e2d6" strokeWidth={1} />
                <text x={padL - 8} y={y(t) + 4} textAnchor="end" fontSize="11" fill="#6b7280">{fmt(t)}</text>
              </g>
            ))}
            {categories.map((c, ci) => {
              const x0 = padL + ci * band + (band - groupW) / 2
              return (
                <g key={c}>
                  {series.map((s, si) => {
                    const v = s.values[ci] ?? 0
                    const h = Math.max(0, padT + plotH - y(v))
                    const x = x0 + si * (barW + gap)
                    const rr = Math.min(r, h, barW / 2)
                    const by = padT + plotH - h
                    // Rounded data end, square at the baseline.
                    const d = h === 0 ? '' : `M${x},${padT + plotH} V${by + rr} Q${x},${by} ${x + rr},${by} H${x + barW - rr} Q${x + barW},${by} ${x + barW},${by + rr} V${padT + plotH} Z`
                    const active = hover?.c === ci && hover.s === si
                    return <path key={s.key} d={d} fill={s.color} opacity={hover && !active ? 0.55 : 1} />
                  })}
                  {/* Generous hit target covering the whole band. */}
                  {series.map((s, si) => (
                    <rect
                      key={`hit-${s.key}`}
                      x={x0 + si * (barW + gap) - gap}
                      y={padT}
                      width={barW + gap * 2}
                      height={plotH}
                      fill="transparent"
                      onMouseEnter={() => setHover({ c: ci, s: si })}
                      onMouseLeave={() => setHover(null)}
                    />
                  ))}
                  <text x={padL + ci * band + band / 2} y={height - 8} textAnchor="middle" fontSize="11" fill="#6b7280">{c}</text>
                </g>
              )
            })}
            <line x1={padL} x2={W - padR} y1={padT + plotH} y2={padT + plotH} stroke="#cfc8b8" strokeWidth={1} />
          </svg>
          {hover && (
            <div
              role="tooltip"
              className="pointer-events-none absolute z-10 -translate-x-1/2 rounded-lg bg-white px-3 py-2 text-xs shadow-lg ring-1 ring-cream-300"
              style={{ left: `${((padL + hover.c * band + band / 2) / W) * 100}%`, top: 0 }}
            >
              <div className="font-semibold text-brand-900">{categories[hover.c]}</div>
              {series.map((s) => (
                <div key={s.key} className="flex items-center gap-2 tabular-nums text-slate-700">
                  <span className="inline-block h-2 w-2 rounded-sm" style={{ background: s.color }} />
                  {s.label}: {unit} {fmt2(s.values[hover.c] ?? 0)}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
