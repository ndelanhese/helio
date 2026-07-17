import { ChartNoAxesCombined, Clock3, Gauge, Settings, WalletCards } from 'lucide-react'

const destinations = [
  { href: '/', icon: Gauge, label: 'Agora' },
  { href: '/history', icon: Clock3, label: 'Histórico' },
  { href: '/insights', icon: ChartNoAxesCombined, label: 'Insights' },
  { href: '/finance', icon: WalletCards, label: 'Financeiro' },
  { href: '/settings', icon: Settings, label: 'Configurações' },
]

export function PrimaryNav({ currentPath = window.location.pathname }: { currentPath?: string }) {
  return (
    <nav aria-label="Principal" className="primary-nav">
      {destinations.map(({ href, icon: Icon, label }) => {
        const current = href === '/' ? currentPath === href : currentPath.startsWith(href)
        return (
          <a aria-current={current ? 'page' : undefined} href={href} key={href}>
            <Icon aria-hidden="true" />
            <span>{label}</span>
          </a>
        )
      })}
    </nav>
  )
}
