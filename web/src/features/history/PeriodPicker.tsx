import type { HistoryPeriod } from './history-model'

const PERIODS: Array<{ label: string; value: HistoryPeriod }> = [
  { label: 'Dia', value: 'day' },
  { label: 'Semana', value: 'week' },
  { label: 'Mês', value: 'month' },
  { label: 'Ano', value: 'year' },
]

export function PeriodPicker({ onChange, value }: { onChange: (period: HistoryPeriod) => void; value: HistoryPeriod }) {
  return (
    <fieldset className="period-picker">
      <legend className="sr-only">Período do histórico</legend>
      {PERIODS.map((period) => (
        <button aria-pressed={value === period.value} key={period.value} onClick={() => onChange(period.value)} type="button">
          {period.label}
        </button>
      ))}
    </fieldset>
  )
}
