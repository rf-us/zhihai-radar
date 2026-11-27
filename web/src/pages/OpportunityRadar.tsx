import type { OpportunityScores } from '../types'

export const dimensions = [
  { key: 'demand', code: 'D1', label: '需求热度', color: 'var(--dim-demand)' },
  { key: 'pain', code: 'D2', label: '痛点强度', color: 'var(--dim-pain)' },
  { key: 'trend', code: 'D3', label: '趋势信号', color: 'var(--dim-trend)' },
  { key: 'competition', code: 'D4', label: '竞争供给', color: 'var(--dim-compete)' },
  { key: 'cost', code: 'D5', label: '成本可行性', color: 'var(--dim-cost)' },
] as const

const center = { x: 180, y: 148 }

function point(index: number, score: number) {
  const angle = index * 2 * Math.PI / dimensions.length - Math.PI / 2
  return {
    x: center.x + Math.cos(angle) * score,
    y: center.y + Math.sin(angle) * score,
  }
}

function coordinates(position: ReturnType<typeof point>) {
  return `${position.x},${position.y}`
}

export default function OpportunityRadar({ scores }: { scores: OpportunityScores }) {
  const vertices = dimensions.map(({ key }, index) => {
    const score = scores[key]
    return score === null ? null : point(index, score)
  })
  const hasMissing = vertices.some((vertex) => vertex === null)

  return (
    <svg className="opportunity-radar" viewBox="0 0 360 284" role="img" aria-labelledby="opportunity-radar-title opportunity-radar-description">
      <title id="opportunity-radar-title">五维评分雷达</title>
      <desc id="opportunity-radar-description">
        {dimensions.map(({ key, label }) => `${label} ${scores[key] ?? '未测算'}`).join('，')}
      </desc>
      {[25, 50, 75, 100].map((level) => (
        <polygon key={level} className="opportunity-radar__grid"
          points={dimensions.map((_, index) => coordinates(point(index, level))).join(' ')} />
      ))}
      {dimensions.map(({ key, color }, index) => {
        const end = point(index, 100)
        const missing = scores[key] === null
        return (
          <line key={key} x1={center.x} y1={center.y} x2={end.x} y2={end.y}
            stroke={missing ? 'var(--c-text-subtle)' : color}
            className="opportunity-radar__axis" strokeDasharray={missing ? '4 4' : undefined} />
        )
      })}
      {hasMissing ? vertices.map((vertex, index) => {
        const next = vertices[(index + 1) % vertices.length]
        return vertex && next ? (
          <line key={dimensions[index].key} className="opportunity-radar__profile"
            x1={vertex.x} y1={vertex.y} x2={next.x} y2={next.y} />
        ) : null
      }) : (
        <polygon className="opportunity-radar__profile opportunity-radar__profile--closed"
          points={vertices.flatMap((vertex) => vertex ? [coordinates(vertex)] : []).join(' ')} />
      )}
      {dimensions.map(({ key, label, color }, index) => {
        const score = scores[key]
        const vertex = vertices[index]
        const text = point(index, 139)
        return (
          <g key={key} data-dimension={key}>
            {vertex && <circle cx={vertex.x} cy={vertex.y} r="4" stroke={color} fill={color} strokeWidth="2" />}
            <text x={text.x} y={text.y} textAnchor="middle" className="opportunity-radar__label">
              {label}
              <tspan x={text.x} dy="18" fill={score === null ? 'var(--c-text-muted)' : color}>
                {score ?? '未测算'}
              </tspan>
            </text>
          </g>
        )
      })}
    </svg>
  )
}
