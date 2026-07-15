import { CartesianGrid, Line, LineChart, ReferenceArea, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { buildChartRows, type ChartRow, type HistoryRange, type HistoryView } from './history-model'
import { formatPower } from './SummaryCards'

function ChartTooltip({ active, payload, timezone }: { active?: boolean; payload?: Array<{ dataKey?: string; payload?: ChartRow; value?: number }>; timezone: string }) {
  const row = payload?.[0]?.payload
  if (active && row?.gapLabel) return <div className="chart-tooltip"><strong>Sem dados</strong><span>Intervalo sem leituras</span></div>
  const item = payload?.find((entry) => typeof entry.value === 'number')
  if (!active || !item?.dataKey || !item.payload) return null
  const sourceAt = item.payload[`${item.dataKey}At`]
  if (typeof sourceAt !== 'string') return null
  const time = new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone }).format(new Date(sourceAt))
  return <div className="chart-tooltip"><strong>{formatPower(item.value ?? 0)}</strong><span>{time}</span></div>
}

export function ProductionChart({ current, previous, range, timezone }: { current: HistoryView; previous?: HistoryView; range: HistoryRange; timezone: string }) {
  const data = buildChartRows(current, previous, range)
  const hours = new Intl.DateTimeFormat('pt-BR', { day: range.period === 'day' ? undefined : '2-digit', hour: range.period === 'day' ? '2-digit' : undefined, month: range.period === 'year' ? 'short' : undefined, timeZone: timezone })
  return (
    <section aria-label="Curva de produção" className="production-chart">
      <div aria-hidden="true" className="chart-frame">
        <ResponsiveContainer height={320} width="100%">
          <LineChart data={data} margin={{ bottom: 8, left: 2, right: 12, top: 20 }}>
            <CartesianGrid stroke="var(--border)" vertical={false} />
            <XAxis dataKey="at" domain={[Date.parse(range.from), Date.parse(range.to)]} scale="time" tickFormatter={(value) => hours.format(new Date(value))} type="number" />
            <YAxis tickFormatter={(value) => value >= 1000 ? `${numberCompact(value / 1000)}k` : String(value)} width={42} />
            <Tooltip content={<ChartTooltip timezone={timezone} />} filterNull={false} />
            {current.gaps.map((gap) => <ReferenceArea fill="var(--surface)" fillOpacity={0.72} key={`${gap.from}-${gap.to}`} stroke="var(--muted)" strokeDasharray="6 5" x1={Date.parse(gap.from)} x2={Date.parse(gap.to)} />)}
            {current.segments.map((segment, index) => <Line activeDot={{ r: 5 }} animationDuration={0} dataKey={`current${index}`} dot={false} isAnimationActive={false} key={`current-${segment[0]?.at}`} stroke="var(--accent)" strokeWidth={3} type="linear" />)}
            {previous?.segments.map((segment, index) => <Line animationDuration={0} dataKey={`previous${index}`} dot={false} isAnimationActive={false} key={`previous-${segment[0]?.at}`} stroke="var(--muted)" strokeDasharray="4 5" strokeWidth={1.5} type="linear" />)}
          </LineChart>
        </ResponsiveContainer>
      </div>
      <div className="chart-legend"><span><i className="current-line" />Período atual</span><span><i className="previous-line" />Período anterior</span></div>
      {current.gaps.length > 0 && <ul className="gap-list">
        {current.gaps.map((gap) => {
          const format = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', hour12: false, minute: '2-digit', timeZone: timezone })
          const label = `Sem dados entre ${format.format(new Date(gap.from))} e ${format.format(new Date(gap.to))}`
          return <li key={`${gap.from}-${gap.to}`}><svg aria-label={label} height="12" role="img" strokeDasharray="6 5" width="30"><title>Intervalo sem leituras</title><rect fill="none" height="10" stroke="currentColor" strokeDasharray="6 5" width="28" x="1" y="1" /></svg>{label}</li>
        })}
      </ul>}
    </section>
  )
}

function numberCompact(value: number) {
  return new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 }).format(value)
}
