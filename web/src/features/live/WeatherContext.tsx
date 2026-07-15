import { CloudSun, Info } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { componentHealthQuery } from '../../api/queries'
import type { LiveSnapshot, Settings } from '../../api/types'

const weatherLabels: Record<number, string> = {
	0: 'Céu limpo', 1: 'Poucas nuvens', 2: 'Parcialmente nublado', 3: 'Nublado', 45: 'Neblina', 48: 'Neblina com geada',
	51: 'Garoa leve', 53: 'Garoa moderada', 55: 'Garoa forte', 61: 'Chuva moderada', 63: 'Chuva moderada', 65: 'Chuva forte',
	71: 'Neve leve', 73: 'Neve moderada', 75: 'Neve forte', 80: 'Pancadas leves', 81: 'Pancadas moderadas', 82: 'Pancadas fortes',
	95: 'Trovoada', 96: 'Trovoada com granizo', 99: 'Trovoada com granizo',
}

const format = (value: number, maximumFractionDigits = 1) => new Intl.NumberFormat('pt-BR', { maximumFractionDigits, minimumFractionDigits: maximumFractionDigits }).format(value)

export function WeatherContext({ snapshot, settings }: { snapshot: LiveSnapshot; settings?: Settings }) {
	const [showModelNote, setShowModelNote] = useState(false)
	const health = useQuery(componentHealthQuery)
	const status = health.data?.weather ?? 'unavailable'
	const cloudCover = health.data?.cloudCoverPct
	const temperature = health.data?.temperatureC
	const precipitation = health.data?.precipitationMM
	const weatherCode = health.data?.weatherCode
	const windSpeed = health.data?.windSpeedKMH
	const irradiance = health.data?.irradianceWM2
	const installedPowerW = settings?.installedPowerW ?? (settings ? settings.panelCount * settings.panelWattage : 0)
	const potentialPowerW = irradiance === undefined || irradiance < 50 || installedPowerW <= 0 ? undefined : installedPowerW * irradiance / 1000
	const potentialRatio = potentialPowerW && potentialPowerW > 0 ? snapshot.acPowerW / potentialPowerW : undefined
	const updatedAt = status === 'available' ? (health.data?.weatherUpdatedAt ?? health.data?.weatherFetchedAt) : health.data?.weatherFetchedAt
	const age = updatedAt ? Math.max(0, Date.now() - new Date(updatedAt).getTime()) : null
	const ageCopy = age === null ? null : age >= 3_600_000
		? `Atualizados há ${Math.floor(age / 3_600_000)} ${Math.floor(age / 3_600_000) === 1 ? 'hora' : 'horas'}.`
		: `Atualizados há ${Math.max(1, Math.floor(age / 60_000))} minutos.`
	const title = status === 'available' && temperature !== undefined ? `${format(temperature)} °C` : status === 'stale' ? 'Dados meteorológicos desatualizados' : 'Previsão indisponível'
	const summary = weatherCode === undefined ? 'Condições atuais' : (weatherLabels[weatherCode] ?? 'Condições atuais')
  return (
    <section className="weather-context" aria-labelledby="weather-title">
      <CloudSun aria-hidden="true" />
      <div><p className="eyebrow">Contexto do céu</p><h2 id="weather-title">{title}</h2>
		{status === 'available' && <><p className="weather-summary">{summary}</p><dl className="weather-readings">
			<div><dt>Chuva</dt><dd>{precipitation === undefined ? '—' : `${format(precipitation)} mm`}</dd></div>
			<div><dt>Vento</dt><dd>{windSpeed === undefined ? '—' : `${format(windSpeed)} km/h`}</dd></div>
			<div><dt>Nuvens</dt><dd>{cloudCover === undefined ? '—' : `${Math.round(cloudCover)}%`}</dd></div>
			<div><dt>Radiação solar</dt><dd>{irradiance === undefined ? 'Sem leitura' : `${Math.round(irradiance)} W/m²`}</dd></div>
		</dl></>}
		{potentialPowerW !== undefined && potentialRatio !== undefined && <div className="solar-comparison"><dl>
			<div><dt>Potencial solar</dt><dd>{`${format(potentialPowerW / 1000, 2)} kW`}</dd></div>
			<div><dt>Produção atual</dt><dd>{`${Math.round(potentialRatio * 100)}% do potencial`}</dd></div>
		</dl><button aria-describedby={showModelNote ? 'solar-model-note' : undefined} aria-label="Como este potencial é estimado" className="model-tooltip-trigger" onBlur={() => setShowModelNote(false)} onFocus={() => setShowModelNote(true)} onMouseEnter={() => setShowModelNote(true)} onMouseLeave={() => setShowModelNote(false)} type="button"><Info aria-hidden="true" /></button>
		{showModelNote && <p className="model-tooltip" id="solar-model-note" role="tooltip">Radiação modelada para sua localização. Não mede o painel e não confirma falha isolada.</p>}</div>}
		{ageCopy && <p className="weather-age tabular-nums">{ageCopy}</p>}
		<p>{status === 'available' ? 'Condições atuais para sua localização.' : status === 'stale' ? 'Confiança meteorológica reduzida' : 'A geração ao vivo continua independente do serviço meteorológico.'}</p>
	  </div>
    </section>
  )
}
