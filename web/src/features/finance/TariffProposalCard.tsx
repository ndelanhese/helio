import type { TariffProposal } from '../../api/types'

function date(value: string) { return new Intl.DateTimeFormat('pt-BR', { dateStyle: 'medium' }).format(new Date(value)) }

export function TariffProposalCard({ proposal, onApprove, pending }: { proposal: TariffProposal; onApprove: () => void; pending: boolean }) {
  return <section className="tariff-proposal" aria-labelledby="tariff-title">
    <p className="eyebrow">Fonte oficial</p><h2 id="tariff-title">Tarifa proposta</h2>
    <p className="tariff-status">Aguardando aprovação explícita</p>
    <p><strong>{proposal.distributor}</strong><br />Vigência {date(proposal.effectiveFrom)} — {date(proposal.effectiveTo)}</p>
    <dl className="tariff-rates"><div><dt>TE + TUSD consumo</dt><dd>{proposal.consumptionTEMicrosPerKWh + proposal.consumptionTUSDMicrosPerKWh} µR$/kWh</dd></div><div><dt>Compensação</dt><dd>{proposal.compensationTEMicrosPerKWh + proposal.compensationTUSDMicrosPerKWh} µR$/kWh</dd></div><div><dt>Disponibilidade</dt><dd>{proposal.availabilityKWh} kWh</dd></div></dl>
    <a href={proposal.sourceUrl} rel="noreferrer" target="_blank">Abrir fonte oficial</a><small>Atualizada em {date(proposal.retrievedAt)} · parser {proposal.parserVersion}</small>
    <button className="primary-action" disabled={pending} onClick={onApprove} type="button">{pending ? 'Aprovando…' : 'Aprovar tarifa'}</button>
  </section>
}
