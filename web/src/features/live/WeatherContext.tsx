import { CloudSun } from 'lucide-react'

export function WeatherContext() {
  return (
    <section className="weather-context" aria-labelledby="weather-title">
      <CloudSun aria-hidden="true" />
      <div><p className="eyebrow">Contexto do céu</p><h2 id="weather-title">Previsão indisponível</h2><p>A geração ao vivo continua independente do serviço meteorológico.</p></div>
    </section>
  )
}
