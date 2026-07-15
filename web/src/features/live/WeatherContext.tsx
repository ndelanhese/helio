import { CloudSun } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'

import { componentHealthQuery } from '../../api/queries'

export function WeatherContext() {
	const health = useQuery(componentHealthQuery)
	const status = health.data?.weather ?? 'unavailable'
	const cloudCover = health.data?.cloudCoverPct
	const irradiance = health.data?.irradianceWM2
	const age = health.data?.weatherFetchedAt ? Math.max(0, Date.now() - new Date(health.data.weatherFetchedAt).getTime()) : null
	const ageCopy = age === null ? null : age >= 3_600_000
		? `Atualizados há ${Math.floor(age / 3_600_000)} ${Math.floor(age / 3_600_000) === 1 ? 'hora' : 'horas'}.`
		: `Atualizados há ${Math.max(1, Math.floor(age / 60_000))} minutos.`
	const title = status === 'available' && cloudCover !== undefined ? `${Math.round(cloudCover)}% de nuvens` : status === 'stale' ? 'Dados meteorológicos desatualizados' : 'Previsão indisponível'
  return (
    <section className="weather-context" aria-labelledby="weather-title">
      <CloudSun aria-hidden="true" />
      <div><p className="eyebrow">Contexto do céu</p><h2 id="weather-title">{title}</h2>
		{status === 'available' && <dl className="weather-readings">
			<dt>Radiação solar</dt><dd>{irradiance === undefined ? '—' : `${Math.round(irradiance)} W/m²`}</dd>
		</dl>}
		{ageCopy && <p className="weather-age tabular-nums">{ageCopy}</p>}
		<p>{status === 'available' ? 'Leitura horária da previsão para sua localização.' : status === 'stale' ? 'Confiança meteorológica reduzida' : 'A geração ao vivo continua independente do serviço meteorológico.'}</p>
	  </div>
    </section>
  )
}
