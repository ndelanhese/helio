import type { TariffProposal } from '../../api/types'

export function TariffProposalCard({ proposal, onApprove, pending }: { proposal: TariffProposal; onApprove: () => void; pending: boolean }) {
  const local = proposal.sourceUrl.startsWith('/')
  return <section className="tariff-proposal" aria-labelledby="tariff-title">
    <p className="eyebrow">{local ? 'Configuração local' : 'Fonte oficial'}</p><h2 id="tariff-title">{local ? 'Tarifa configurada' : 'Tarifa proposta'}</h2>
    <p className="tariff-status">Aguardando aprovação explícita</p>
    <p><strong>{proposal.distributor}</strong><br />Vigência {proposal.effectiveFrom} — {proposal.effectiveTo}</p>
    <dl className="tariff-rates">{proposal.displayRates.map((rate) => <div key={rate.label}><dt>{rate.label}</dt><dd>Atual {rate.approved} → proposta {rate.proposal} ({rate.delta})</dd></div>)}</dl>
    {local ? proposal.sourceUrl === '/settings' ? <a href="/settings">Editar tarifa em Configurações</a> : <small>Valores inseridos da fatura local.</small> : <a href={proposal.sourceUrl} rel="noreferrer" target="_blank">Abrir fonte oficial</a>}<small>Atualizada em {proposal.retrievedAt} · parser {proposal.parserVersion}</small>
    <button className="primary-action" disabled={pending} onClick={onApprove} type="button">{pending ? 'Aprovando…' : 'Aprovar tarifa'}</button>
  </section>
}
