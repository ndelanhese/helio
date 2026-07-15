import { CloudSun } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'

import { componentHealthQuery } from '../../api/queries'

export function WeatherContext() {
	const health = useQuery(componentHealthQuery)
	const status = health.data?.weather ?? 'unavailable'
	const age = health.data?.weatherFetchedAt ? Math.max(0, Date.now() - new Date(health.data.weatherFetchedAt).getTime()) : null
	const ageCopy = age === null ? null : age >= 3_600_000
		? `Atualizados há ${Math.floor(age / 3_600_000)} ${Math.floor(age / 3_600_000) === 1 ? 'hora' : 'horas'}.`
		: `Atualizados há ${Math.max(1, Math.floor(age / 60_000))} minutos.`
	const title = status === 'available' ? 'Dados meteorológicos disponíveis' : status === 'stale' ? 'Dados meteorológicos desatualizados' : 'Previsão indisponível'
  return (
    <section className="weather-context" aria-labelledby="weather-title">
      <CloudSun aria-hidden="true" />
      <div><p className="eyebrow">Contexto do céu</p><h2 id="weather-title">{title}</h2>
		{ageCopy && <p className="weather-age tabular-nums">{ageCopy}</p>}
		<p>{status === 'available' ? 'Confiança meteorológica disponível para contextualizar a geração.' : status === 'stale' ? 'Confiança meteorológica reduzida' : 'A geração ao vivo continua independente do serviço meteorológico.'}</p>
	  </div>
    </section>
  )
}
