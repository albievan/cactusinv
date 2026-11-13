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
const fmtN = (n: number, d: number) => n.toLocaleString(undefined, { minimumFractionDigits: d, maximumFractionDigits: d })

/**
 * ColumnChart draws grouped columns (one group per category) on a single y-axis, with
 * a legend, hairline gridlines, per-column hover tooltips and an accessible table view.
 */
export function ColumnChart({ categories, series, unit, height = 240, decimals = 2 }: { categories: string[]; series: Series[]; unit: string; height?: number; decimals?: number }) {
  const fmt2 = (n: number) => fmtN(n, decimals)
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
      <div className="mb-2 d-flex flex-wrap align-items-center justify-content-between gap-2">
        <ul className="list-unstyled d-flex flex-wrap gap-3 mb-0 small text-body" aria-label="Legend">
          {series.map((s) => (
            <li key={s.key} className="d-flex align-items-center gap-2">
              <span className="d-inline-block rounded-1" style={{ width: 12, height: 12, background: s.color }} aria-hidden="true" />
              {s.label}
            </li>
          ))}
        </ul>
        <button type="button" className="btn btn-link btn-sm p-0 fs-8 fw-semibold" onClick={() => setTable((t) => !t)} aria-pressed={table}>
          {table ? 'Show chart' : 'Show table'}
        </button>
      </div>
      {table ? (
        <div className="table-responsive">
          <table className="table table-sm align-middle mb-0 small">
            <thead>
              <tr>
                <th>Month</th>
                {series.map((s) => <th key={s.key} className="text-end">{s.label} ({unit})</th>)}
              </tr>
            </thead>
            <tbody>
              {categories.map((c, i) => (
                <tr key={c}>
                  <td>{c}</td>
                  {series.map((s) => <td key={s.key} className="text-end" style={{ fontVariantNumeric: 'tabular-nums' }}>{fmt2(s.values[i] ?? 0)}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="position-relative">
          <svg viewBox={`0 0 ${W} ${height}`} className="d-block w-100 h-auto" role="img" aria-label={`${series.map((s) => s.label).join(' and ')} per month, ${unit}`}>
            {ticks.map((t) => (
              <g key={t}>
                <line x1={padL} x2={W - padR} y1={y(t)} y2={y(t)} stroke="var(--bs-border-color)" strokeWidth={1} />
                <text x={padL - 8} y={y(t) + 4} textAnchor="end" fontSize="11" fill="var(--bs-secondary-color)">{fmt(t)}</text>
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
                  <text x={padL + ci * band + band / 2} y={height - 8} textAnchor="middle" fontSize="11" fill="var(--bs-secondary-color)">{c}</text>
                </g>
              )
            })}
            <line x1={padL} x2={W - padR} y1={padT + plotH} y2={padT + plotH} style={{ stroke: 'var(--ci-chart-axis)' }} strokeWidth={1} />
          </svg>
          {hover && (
            <div
              role="tooltip"
              className="pe-none position-absolute z-3 translate-middle-x rounded-2 bg-white px-3 py-2 fs-8 shadow border"
              style={{ left: `${((padL + hover.c * band + band / 2) / W) * 100}%`, top: 0 }}
            >
              <div className="fw-semibold text-primary-emphasis">{categories[hover.c]}</div>
              {series.map((s) => (
                <div key={s.key} className="d-flex align-items-center gap-2 text-body" style={{ fontVariantNumeric: 'tabular-nums' }}>
                  <span className="d-inline-block rounded-1" style={{ width: 8, height: 8, background: s.color }} />
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
