import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import axe from 'axe-core'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { authMemory } from '../../api/client'
import { queryKeys } from '../../api/queries'
import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { OnboardingWizard } from './OnboardingWizard'

async function reachLogger(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Usuário administrador'), 'Admin')
  await user.type(screen.getByLabelText('Senha'), 'uma senha local segura')
  await user.type(screen.getByLabelText('Confirmar senha'), 'uma senha local segura')
  await user.click(screen.getByRole('button', { name: 'Continuar para o logger' }))
}

async function reachPanels(user: ReturnType<typeof userEvent.setup>) {
  await reachLogger(user)
  await user.type(screen.getByLabelText('Endereço IP do logger'), '192.168.1.50')
  await user.type(screen.getByLabelText('Número de série do logger'), '123456789')
  await user.click(screen.getByRole('button', { name: 'Continuar para os painéis' }))
}

describe('OnboardingWizard', () => {
  it.each(['light', 'dark'] as const)('não tem violações axe no tema %s', async (theme) => {
    localStorage.setItem('helio-theme', theme)
    const { container } = renderApp(<OnboardingWizard />)
    const result = await axe.run(container, { rules: { 'color-contrast': { enabled: false } } })
    expect(result.violations).toEqual([])
  })

  it('mantém o segredo mascarado até a revelação explícita', async () => {
    const user = userEvent.setup()
    renderApp(<OnboardingWizard />)
    await reachLogger(user)

    const serial = screen.getByLabelText('Número de série do logger')
    expect(serial).toHaveAttribute('type', 'password')
    await user.click(screen.getByRole('button', { name: 'Mostrar número de série' }))
    expect(serial).toHaveAttribute('type', 'text')
  })

  it('orienta IP privado sem substituir a autoridade do servidor', async () => {
    const user = userEvent.setup()
    renderApp(<OnboardingWizard />)
    await reachLogger(user)

    expect(screen.getByText(/prefira um IPv4 privado/i)).toBeVisible()
    await user.type(screen.getByLabelText('Endereço IP do logger'), '8.8.8.8')
    await user.type(screen.getByLabelText('Número de série do logger'), '123456789')
    await user.click(screen.getByRole('button', { name: 'Continuar para os painéis' }))
    expect(screen.getByRole('heading', { name: 'Descreva os painéis' })).toBeVisible()
  })

  it('recusa senha acima de 128 bytes antes de avançar', async () => {
    const user = userEvent.setup()
    renderApp(<OnboardingWizard />)
    const oversized = 'á'.repeat(65)
    await user.type(screen.getByLabelText('Usuário administrador'), 'Admin')
    await user.type(screen.getByLabelText('Senha'), oversized)
    await user.type(screen.getByLabelText('Confirmar senha'), oversized)
    await user.click(screen.getByRole('button', { name: 'Continuar para o logger' }))

    expect(screen.getByText('Use no máximo 128 bytes.')).toBeVisible()
    expect(screen.getByLabelText('Senha')).toHaveFocus()
  })

  it('bloqueia senhas diferentes e move foco para o primeiro erro', async () => {
    const user = userEvent.setup()
    renderApp(<OnboardingWizard />)

    await user.type(screen.getByLabelText('Usuário administrador'), 'Admin')
    await user.type(screen.getByLabelText('Senha'), 'uma senha local segura')
    await user.type(screen.getByLabelText('Confirmar senha'), 'outra senha local segura')
    await user.click(screen.getByRole('button', { name: 'Continuar para o logger' }))

    expect(screen.getByText('As senhas precisam ser iguais.')).toBeVisible()
    expect(screen.getByLabelText('Confirmar senha')).toHaveFocus()
    expect(screen.getByLabelText('Confirmar senha')).toHaveAttribute('aria-describedby', 'confirmPassword-error')
    expect(screen.getByLabelText('Confirmar senha')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.queryByRole('heading', { name: 'Conecte o logger' })).not.toBeInTheDocument()
  })

  it('deriva 7 × 610 como 4,27 kWp e deixa PV1/PV2 editáveis', async () => {
    const user = userEvent.setup()
    renderApp(<OnboardingWizard />)
    await reachPanels(user)

    expect(screen.getByLabelText('Entrada PV1 ativa')).toBeChecked()
    expect(screen.getByLabelText('Entrada PV2 ativa')).not.toBeChecked()
    expect(screen.getByText('4,27 kWp')).toBeVisible()
    await user.click(screen.getByLabelText('Entrada PV1 ativa'))
    await user.click(screen.getByLabelText('Entrada PV2 ativa'))
    expect(screen.getByLabelText('Entrada PV1 ativa')).not.toBeChecked()
    expect(screen.getByLabelText('Entrada PV2 ativa')).toBeChecked()
  })

  it('envia uma única configuração atômica e mantém somente a sessão em memória', async () => {
    const user = userEvent.setup()
    const onSuccess = vi.fn()
    let submitted: unknown
    let requests = 0
    server.use(http.post('/api/v1/bootstrap', async ({ request }) => {
      requests += 1
      submitted = await request.json()
      return HttpResponse.json({
        userId: 'u1', username: 'Admin', expiresAt: '2026-08-14T00:00:00Z', csrfToken: 'bootstrap-token',
      }, { status: 201 })
    }))
    const { client } = renderApp(<OnboardingWizard onSuccess={onSuccess} />)

    await reachPanels(user)
    await user.click(screen.getByRole('button', { name: 'Continuar para local e tarifa' }))
    await user.clear(screen.getByLabelText('Latitude'))
    await user.type(screen.getByLabelText('Latitude'), '-23.55')
    await user.clear(screen.getByLabelText('Longitude'))
    await user.type(screen.getByLabelText('Longitude'), '-46.63')
    await user.click(screen.getByRole('button', { name: 'Revisar configuração' }))

    const review = screen.getByRole('region', { name: 'Revisão da configuração' })
    expect(within(review).queryByText('uma senha local segura')).not.toBeInTheDocument()
    expect(within(review).queryByText('123456789')).not.toBeInTheDocument()
    await user.dblClick(screen.getByRole('button', { name: 'Criar Helio' }))

    await waitFor(() => expect(onSuccess).toHaveBeenCalledOnce())
    expect(requests).toBe(1)
    expect(submitted).toMatchObject({
      username: 'Admin',
      settings: {
        loggerHost: '192.168.1.50', loggerSerial: '123456789', loggerPort: 8899, modbusSlave: 1,
        panelCount: 7, panelWattage: 610, activeMPPT: [1], currency: 'BRL',
        tariffMinorPerKWh: 95, retentionDays: 730,
      },
    })
    expect(authMemory.getCSRFToken()).toBe('bootstrap-token')
    expect(client.getQueryData(queryKeys.session)).toMatchObject({ username: 'Admin' })
    expect(localStorage.getItem('csrfToken')).toBeNull()
    expect(localStorage.getItem('password')).toBeNull()
  })

  it('mapeia um erro 422 do servidor para o campo e preserva os dados', async () => {
    const user = userEvent.setup()
    server.use(http.post('/api/v1/bootstrap', () => HttpResponse.json(
      { error: { code: 'invalid_settings', message: 'logger serial must be a decimal uint32' } },
      { status: 422 },
    )))
    renderApp(<OnboardingWizard />)
    await reachPanels(user)
    await user.click(screen.getByRole('button', { name: 'Continuar para local e tarifa' }))
    await user.click(screen.getByRole('button', { name: 'Revisar configuração' }))
    await user.click(screen.getByRole('button', { name: 'Criar Helio' }))

    expect(await screen.findByText(/número de série precisa conter apenas dígitos/i)).toBeVisible()
    expect(screen.getByLabelText('Número de série do logger')).toHaveValue('123456789')
  })
})
