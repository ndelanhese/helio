import { useQuery, useQueryClient } from '@tanstack/react-query'

import { approveTariffProposal, createBillingCycle, financeSummaryQuery, queryKeys, tariffProposalsQuery } from '../../api/queries'
import type { FinanceSummary, FinancialProjection } from '../../api/types'
import { BillingCycleForm } from './BillingCycleForm'
import { TariffProposalCard } from './TariffProposalCard'

function Projection({ summary, projection }: { summary: FinanceSummary; projection: FinancialProjection | null }) { if (!projection) return <section className="finance-guidance"><h2>Uma tarifa aprovada abre as projeções</h2><p>Revise a proposta de fonte oficial e aprove-a antes de registrar a primeira fatura.</p></section>; return <><section className="finance-hero"><div><p>Projeção estimada sem medidor</p><h1>{projection.displayTotal}</h1><span>Sem solar: {projection.displayWithoutSolar}</span></div><div><p>Saldo de créditos</p><strong>{summary.creditBalanceKWh} kWh</strong><span>Próximo vencimento: {summary.nextCreditExpiry ?? 'sem créditos a vencer'}</span></div></section><section className="projection-breakdown"><h2>Real versus projetado</h2><p>A composição é estimada pelo servidor; o total informado na conta é a referência.</p><dl>{projection.displayRows.map((row) => <div key={row.label}><dt>{row.label}</dt><dd>{row.value}</dd></div>)}</dl></section></> }

export function FinancePage() {
  const client = useQueryClient(); const summary = useQuery(financeSummaryQuery); const proposals = useQuery(tariffProposalsQuery); const proposal = proposals.data?.proposals.find((item) => item.approvedAt === null); const approved = proposals.data?.proposals.some((item) => item.approvedAt !== null) ?? Boolean(summary.data?.cycles.length)
  const refresh = async () => { await Promise.all([client.invalidateQueries({ queryKey: queryKeys.finance }), client.invalidateQueries({ queryKey: queryKeys.tariffProposals })]) }
  if (summary.isPending || proposals.isPending) return <section aria-busy="true" className="finance-loading">Carregando registros financeiros…</section>
  if (summary.isError || proposals.isError || !summary.data || !proposals.data) return <section className="finance-state"><h1>Não foi possível carregar o financeiro.</h1><button className="secondary-action" onClick={() => { void refresh() }} type="button">Tentar novamente</button></section>
  return <article className="finance-page"><header><p className="eyebrow">Unidade geradora</p><h1>Financeiro solar</h1></header><Projection summary={summary.data} projection={summary.data.latestProjection} />{proposal && <TariffProposalCard pending={false} proposal={proposal} onApprove={() => { void approveTariffProposal(proposal.id).then(refresh) }} />}{!proposal && !approved && <section className="finance-guidance"><h2>Sem tarifa aprovada</h2><p>Não há uma tarifa oficial disponível. Atualize a fonte da distribuidora antes de lançar a conta.</p></section>}<BillingCycleForm disabled={!approved} onSave={async (payload) => { await createBillingCycle(payload); await refresh() }} /></article>
}
