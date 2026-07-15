import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { authMemory, configureUnauthorizedHandler } from '../../api/client'
import { queryKeys } from '../../api/queries'
import { authenticatedSession } from '../../test/fixtures'
import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { SettingsPage } from './SettingsPage'

const settings = {
  activeMPPT: [1], currency: 'BRL', installedPowerW: 4_270, latitude: -23.5, longitude: -46.6,
  loggerHost: '192.168.1.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}

const health = {
  collector: 'running', database: 'ok', logger: 'online', weather: 'stale',
  collectorUpdatedAt: '2026-07-15T02:00:00Z', databaseUpdatedAt: '2026-07-15T02:00:00Z',
  loggerUpdatedAt: '2026-07-15T02:00:00Z', weatherUpdatedAt: '2026-07-15T01:00:00Z',
}

function useSettingsHandlers() {
  server.use(
    http.get('/api/v1/settings', () => HttpResponse.json(settings)),
    http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
    http.get('/health/components', () => HttpResponse.json(health)),
  )
}

afterEach(() => {
  authMemory.clear()
  configureUnauthorizedHandler(() => { authMemory.clear() })
  vi.restoreAllMocks()
})

describe('SettingsPage', () => {
  it('adopts a refreshed server document while the form is pristine', async () => {
    let current = settings
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(current)),
      http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
      http.get('/health/components', () => HttpResponse.json(health)),
    )
    const { client } = renderApp(<SettingsPage />)
    expect(await screen.findByRole('spinbutton', { name: 'Potência por painel (W)' })).toHaveValue(610)

    current = { ...settings, panelWattage: 650, installedPowerW: 4_550 }
    act(() => {
      client.setQueryData(queryKeys.settings, current)
    })

    await waitFor(() => expect(screen.getByRole('spinbutton', { name: 'Potência por painel (W)' })).toHaveValue(650))
    expect(screen.getByText('4,55 kWp')).toBeVisible()
  })

  it('retains dirty edits across a nonidentity refetch and lets the user keep them', async () => {
    let current = settings
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(current)),
      http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
      http.get('/health/components', () => HttpResponse.json(health)),
    )
    const { client } = renderApp(<SettingsPage />)
    const wattage = await screen.findByRole('spinbutton', { name: 'Potência por painel (W)' })
    await userEvent.clear(wattage)
    await userEvent.type(wattage, '620')

    current = { ...settings, panelCount: 8, installedPowerW: 4_880 }
    act(() => {
      client.setQueryData(queryKeys.settings, current)
    })

    const conflict = await screen.findByRole('status', { name: 'Configurações alteradas no servidor' })
    expect(within(conflict).getByText(/foram alteradas em outra sessão/i)).toBeVisible()
    expect(wattage).toHaveValue(620)
    expect(screen.getByRole('spinbutton', { name: 'Quantidade de painéis' })).toHaveValue(7)
    await userEvent.click(within(conflict).getByRole('button', { name: 'Manter minhas edições' }))
    expect(screen.queryByRole('status', { name: 'Configurações alteradas no servidor' })).not.toBeInTheDocument()
    expect(wattage).toHaveValue(620)
  })

  it('does not remount dirty fields when logger identity changes on the server and can reload safely', async () => {
    let current = settings
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(current)),
      http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
      http.get('/health/components', () => HttpResponse.json(health)),
    )
    const { client } = renderApp(<SettingsPage />)
    const wattage = await screen.findByRole('spinbutton', { name: 'Potência por painel (W)' })
    await userEvent.clear(wattage)
    await userEvent.type(wattage, '620')

    current = { ...settings, loggerHost: '192.168.1.60', loggerSerial: '456' }
    act(() => {
      client.setQueryData(queryKeys.settings, current)
    })

    expect(wattage).toHaveValue(620)
    expect(screen.getByRole('textbox', { name: 'Endereço IP do logger' })).toHaveValue('192.168.1.50')
    const conflict = await screen.findByRole('status', { name: 'Configurações alteradas no servidor' })
    await userEvent.click(within(conflict).getByRole('button', { name: 'Carregar alterações do servidor' }))
    expect(screen.getByRole('textbox', { name: 'Endereço IP do logger' })).toHaveValue('192.168.1.60')
    expect(wattage).toHaveValue(610)
    expect(screen.queryByLabelText('Senha atual')).not.toBeInTheDocument()
  })

  it('preserves the edit during a server refetch racing a successful save and rebases afterward', async () => {
    let current = settings
    let finishSave: ((value: typeof settings) => void) | undefined
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(current)),
      http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
      http.get('/health/components', () => HttpResponse.json(health)),
      http.put('/api/v1/settings', async () => new Promise<Response>((resolve) => {
        finishSave = (value) => resolve(HttpResponse.json(value))
      })),
    )
    const { client } = renderApp(<SettingsPage />)
    const wattage = await screen.findByRole('spinbutton', { name: 'Potência por painel (W)' })
    await userEvent.clear(wattage)
    await userEvent.type(wattage, '620')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(await screen.findByRole('button', { name: 'Salvando configurações…' })).toBeDisabled()

    current = { ...settings, panelCount: 8, installedPowerW: 4_880 }
    act(() => {
      client.setQueryData(queryKeys.settings, current)
    })
    expect(wattage).toHaveValue(620)
    expect(await screen.findByRole('status', { name: 'Configurações alteradas no servidor' })).toBeVisible()

    current = { ...settings, panelWattage: 620, installedPowerW: 4_340 }
    finishSave?.(current)
    expect(await screen.findByText('Configurações salvas.')).toBeVisible()
    expect(screen.queryByRole('status', { name: 'Configurações alteradas no servidor' })).not.toBeInTheDocument()
    expect(wattage).toHaveValue(620)
  })

  it('locks conflict choices during save and enables them after a failed mutation settles', async () => {
    let finishSave: (() => void) | undefined
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(settings)),
      http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
      http.get('/health/components', () => HttpResponse.json(health)),
      http.put('/api/v1/settings', async () => new Promise<Response>((resolve) => {
        finishSave = () => resolve(HttpResponse.json(
          { error: { code: 'settings_conflict', message: 'settings changed' } },
          { status: 409 },
        ))
      })),
    )
    const { client } = renderApp(<SettingsPage />)
    const wattage = await screen.findByRole('spinbutton', { name: 'Potência por painel (W)' })
    await userEvent.clear(wattage)
    await userEvent.type(wattage, '620')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))

    act(() => {
      client.setQueryData(queryKeys.settings, { ...settings, panelCount: 8, installedPowerW: 4_880 })
    })
    const conflict = await screen.findByRole('status', { name: 'Configurações alteradas no servidor' })
    const reload = within(conflict).getByRole('button', { name: 'Carregar alterações do servidor' })
    const keep = within(conflict).getByRole('button', { name: 'Manter minhas edições' })
    expect(within(conflict).getByText(/aguarde o salvamento terminar/i)).toBeVisible()
    expect(reload).toBeDisabled()
    expect(keep).toBeDisabled()

    await userEvent.click(reload)
    keep.focus()
    await userEvent.keyboard('{Enter}')
    expect(wattage).toHaveValue(620)
    expect(screen.getByRole('spinbutton', { name: 'Quantidade de painéis' })).toHaveValue(7)

    finishSave?.()
    expect(await screen.findByText('As configurações mudaram em outra sessão. Recarregue e tente novamente.')).toBeVisible()
    expect(reload).toBeEnabled()
    expect(keep).toBeEnabled()
    await userEvent.click(reload)
    expect(screen.getByRole('spinbutton', { name: 'Quantidade de painéis' })).toHaveValue(8)
    expect(wattage).toHaveValue(610)
    expect(screen.queryByRole('status', { name: 'Configurações alteradas no servidor' })).not.toBeInTheDocument()
  })

  it('shows the initial loading state and retries a failed settings request without inventing values', async () => {
    let requests = 0
    server.use(
      http.get('/api/v1/settings', () => {
        requests += 1
        return requests === 1 ? HttpResponse.error() : HttpResponse.json(settings)
      }),
      http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
      http.get('/health/components', () => HttpResponse.json(health)),
    )
    renderApp(<SettingsPage />)
    expect(screen.getByText('Carregando configurações locais…')).toBeVisible()
    expect(await screen.findByRole('heading', { name: 'Não foi possível carregar as configurações.' })).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Tentar carregar configurações' }))
    expect(await screen.findByText('4,27 kWp')).toBeVisible()
  })

  it('edits the array configuration, derives capacity, and invalidates only dependent queries after CSRF save', async () => {
    useSettingsHandlers()
    let current = settings
    const requests: Array<{ body: unknown; csrf: string | null }> = []
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(current)),
      http.put('/api/v1/settings', async ({ request }) => {
        requests.push({ body: await request.json(), csrf: request.headers.get('X-CSRF-Token') })
        current = { ...settings, panelCount: 8, panelWattage: 500, activeMPPT: [1, 2], installedPowerW: 4_000 }
        return HttpResponse.json(current)
      }),
    )
    const { client } = renderApp(<SettingsPage />)
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    expect(await screen.findByText('7 × 610 W')).toBeVisible()
    expect(screen.getByText('4,27 kWp')).toBeVisible()
    const count = screen.getByRole('spinbutton', { name: 'Quantidade de painéis' })
    const wattage = screen.getByRole('spinbutton', { name: 'Potência por painel (W)' })
    await userEvent.clear(count)
    await userEvent.type(count, '8')
    await userEvent.clear(wattage)
    await userEvent.type(wattage, '500')
    await userEvent.click(screen.getByRole('checkbox', { name: 'PV2' }))
    expect(screen.getByText('8 × 500 W')).toBeVisible()
    expect(screen.getByText('4,00 kWp')).toBeVisible()

    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(await screen.findByText('Configurações salvas.')).toBeVisible()
    expect(requests).toHaveLength(1)
    expect(requests[0]?.csrf).toBe(authenticatedSession.csrfToken)
    expect(requests[0]?.body).toMatchObject({ panelCount: 8, panelWattage: 500, activeMPPT: [1, 2] })
    expect(requests[0]?.body).not.toHaveProperty('installedPowerW')
    expect(invalidate).toHaveBeenCalledTimes(3)
    expect(invalidate.mock.calls.map(([options]) => options?.queryKey)).toEqual([
      queryKeys.settings, queryKeys.live, queryKeys.health,
    ])
  })

  it('strictly validates numeric fields and blocks an invalid or duplicate submission', async () => {
    useSettingsHandlers()
    let saves = 0
    let release: (() => void) | undefined
    server.use(http.put('/api/v1/settings', async () => {
      saves += 1
      await new Promise<void>((resolve) => { release = resolve })
      return HttpResponse.json(settings)
    }))
    renderApp(<SettingsPage />)
    const count = await screen.findByRole('spinbutton', { name: 'Quantidade de painéis' })
    await userEvent.clear(count)
    await userEvent.type(count, '1.5')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(screen.getByText('Informe uma quantidade inteira positiva de painéis.')).toBeVisible()
    expect(saves).toBe(0)

    await userEvent.clear(count)
    await userEvent.type(count, '7')
    const save = screen.getByRole('button', { name: 'Salvar configurações' })
    await userEvent.click(save)
    await userEvent.click(save)
    expect(await screen.findByRole('button', { name: 'Salvando configurações…' })).toBeDisabled()
    expect(saves).toBe(1)
    release?.()
    expect(await screen.findByText('Configurações salvas.')).toBeVisible()
  })

  it('associates a strict retention error with the data field', async () => {
    useSettingsHandlers()
    renderApp(<SettingsPage />)
    const retention = await screen.findByRole('spinbutton', { name: 'Retenção do histórico (dias)' })
    await userEvent.clear(retention)
    await userEvent.type(retention, '29')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(screen.getByText('Escolha um número inteiro entre 30 e 3650 dias.')).toBeVisible()
    expect(retention).toHaveAttribute('aria-invalid', 'true')
    expect(retention).toHaveAccessibleDescription('Escolha um número inteiro entre 30 e 3650 dias.')
  })

  it('submits retention changes with Enter from the data field', async () => {
    useSettingsHandlers()
    let requests = 0
    server.use(http.put('/api/v1/settings', async ({ request }) => {
      requests += 1
      return HttpResponse.json({ ...settings, ...await request.json() as object })
    }))
    renderApp(<SettingsPage />)
    const retention = await screen.findByRole('spinbutton', { name: 'Retenção do histórico (dias)' })
    expect(retention).toHaveProperty('form', document.getElementById('settings-form'))
    await userEvent.clear(retention)
    await userEvent.type(retention, '900{Enter}')
    expect(await screen.findByText('Configurações salvas.')).toBeVisible()
    expect(requests).toBe(1)
  })

  it('re-authenticates a logger identity change, rotates CSRF in memory, and never sends password to settings', async () => {
    useSettingsHandlers()
    let current = settings
    let loginBody: unknown
    let updateBody: unknown
    let updateCSRF: string | null = null
    server.use(
      http.post('/api/v1/auth/login', async ({ request }) => {
        loginBody = await request.json()
        return HttpResponse.json({ ...authenticatedSession, csrfToken: 'rotated-csrf' })
      }),
      http.put('/api/v1/settings', async ({ request }) => {
        updateBody = await request.json()
        updateCSRF = request.headers.get('X-CSRF-Token')
        current = { ...settings, loggerHost: '192.168.1.60' }
        return HttpResponse.json(current)
      }),
      http.get('/api/v1/settings', () => HttpResponse.json(current)),
    )
    renderApp(<SettingsPage />)
    const host = await screen.findByRole('textbox', { name: 'Endereço IP do logger' })
    await userEvent.clear(host)
    await userEvent.type(host, '192.168.1.60')
    expect(screen.getByText(/confirme a senha atual/i)).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(screen.getByText('Informe a senha atual para alterar a identidade do logger.')).toBeVisible()

    await userEvent.type(screen.getByLabelText('Senha atual'), 'senha somente na memória')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(await screen.findByText('Configurações salvas.')).toBeVisible()
    expect(loginBody).toEqual({ username: authenticatedSession.username, password: 'senha somente na memória' })
    expect(updateBody).not.toHaveProperty('currentPassword')
    expect(updateBody).not.toHaveProperty('password')
    expect(updateCSRF).toBe('rotated-csrf')
    expect(authMemory.getCSRFToken()).toBe('rotated-csrf')
    expect(localStorage.getItem('senha somente na memória')).toBeNull()
    expect(screen.queryByLabelText('Senha atual')).not.toBeInTheDocument()
  })

  it('preserves entered values and maps a 422 field error accessibly', async () => {
    useSettingsHandlers()
    server.use(http.put('/api/v1/settings', () => HttpResponse.json(
      { error: { code: 'invalid_settings', message: 'panel wattage must be positive' } },
      { status: 422 },
    )))
    renderApp(<SettingsPage />)
    const wattage = await screen.findByRole('spinbutton', { name: 'Potência por painel (W)' })
    await userEvent.clear(wattage)
    await userEvent.type(wattage, '700')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))

    expect(await screen.findByText('Revise a potência por painel.')).toBeVisible()
    expect(wattage).toHaveValue(700)
    expect(wattage).toHaveAttribute('aria-invalid', 'true')
  })

  it('keeps edits after a network failure and offers a safe retry through the same save action', async () => {
    useSettingsHandlers()
    server.use(http.put('/api/v1/settings', () => HttpResponse.error()))
    renderApp(<SettingsPage />)
    const wattage = await screen.findByRole('spinbutton', { name: 'Potência por painel (W)' })
    await userEvent.clear(wattage)
    await userEvent.type(wattage, '620')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(await screen.findByText('Não foi possível alcançar o Helio. Verifique a conexão e tente novamente.')).toBeVisible()
    expect(wattage).toHaveValue(620)
    expect(screen.getByRole('button', { name: 'Salvar configurações' })).toBeEnabled()
  })

  it('handles a 401 while confirming the current password without leaking the secret', async () => {
    useSettingsHandlers()
    const unauthorized = vi.fn()
    configureUnauthorizedHandler(unauthorized)
    authMemory.setCSRFToken('session-csrf')
    server.use(http.post('/api/v1/auth/login', () => HttpResponse.json({ error: { code: 'invalid_credentials', message: 'invalid' } }, { status: 401 })))
    renderApp(<SettingsPage />)
    const host = await screen.findByRole('textbox', { name: 'Endereço IP do logger' })
    await userEvent.clear(host)
    await userEvent.type(host, '192.168.1.60')
    await userEvent.type(screen.getByLabelText('Senha atual'), 'não persistir esta senha')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(await screen.findByText('A senha atual não foi confirmada. Tente novamente.')).toBeVisible()
    expect(screen.getByLabelText('Senha atual')).toHaveValue('não persistir esta senha')
    expect(JSON.stringify(localStorage)).not.toContain('não persistir esta senha')
    expect(unauthorized).not.toHaveBeenCalled()
    expect(authMemory.getCSRFToken()).toBe(authenticatedSession.csrfToken)
  })

  it.each([
    [409, 'As configurações mudaram em outra sessão. Recarregue e tente novamente.'],
    [500, 'O Helio não conseguiu salvar as configurações. Tente novamente.'],
  ])('keeps the form available after a %s response', async (status, message) => {
    useSettingsHandlers()
    server.use(http.put('/api/v1/settings', () => HttpResponse.json({ error: { code: 'request_failed', message: 'unsafe server detail' } }, { status })))
    renderApp(<SettingsPage />)
    await screen.findByDisplayValue('610')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar configurações' }))
    expect(await screen.findByText(message)).toBeVisible()
    expect(screen.getByRole('spinbutton', { name: 'Potência por painel (W)' })).toHaveValue(610)
  })

  it('shows independent process, database, logger, and weather health with retry states', async () => {
    let healthRequests = 0
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(settings)),
      http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
      http.get('/health/components', () => {
        healthRequests += 1
        return healthRequests === 1
          ? HttpResponse.error()
          : HttpResponse.json(health)
      }),
    )
    renderApp(<SettingsPage />)
    expect(await screen.findByText('Não foi possível consultar a conexão.')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Tentar consultar conexão' }))
    const connection = await screen.findByRole('region', { name: 'Estado da conexão' })
    expect(within(connection).getByText('Processo')).toBeVisible()
    expect(within(connection).getByText('Banco de dados')).toBeVisible()
    expect(within(connection).getByText('Logger')).toBeVisible()
    expect(within(connection).getByText('Clima')).toBeVisible()
    expect(within(connection).getByText('Desatualizado')).toBeVisible()
  })

  it('offers only the three existing theme choices and no future service controls', async () => {
    useSettingsHandlers()
    renderApp(<SettingsPage />)
    await screen.findByRole('heading', { name: 'Luz para cada momento.' })
    await userEvent.click(screen.getByRole('radio', { name: 'Escuro' }))
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('helio.theme.v1')).toBe('dark')
    expect(screen.queryByText(new RegExp(['tele', 'gram'].join(''), 'i'))).not.toBeInTheDocument()
    expect(screen.queryByText(new RegExp(['acesso', 'remoto'].join(' '), 'i'))).not.toBeInTheDocument()
  })

  it('downloads an authenticated consistent backup through a Blob URL and revokes it', async () => {
    useSettingsHandlers()
    let requestedURL = ''
    let credentials = ''
    server.use(http.get('/api/v1/data/backup', ({ request }) => {
      requestedURL = request.url
      credentials = request.credentials
      return new HttpResponse(new Uint8Array([83, 81, 76]), {
        headers: {
          'Content-Disposition': 'attachment; filename="helio-backup-20260715-020000.db"',
          'Content-Type': 'application/vnd.sqlite3',
        },
      })
    }))
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:helio-backup')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    renderApp(<SettingsPage />)
    await screen.findByRole('heading', { name: 'Seu histórico continua portátil.' })
    await userEvent.click(screen.getByRole('button', { name: 'Baixar backup consistente' }))

    await waitFor(() => expect(click).toHaveBeenCalledOnce())
    expect(createObjectURL).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:helio-backup')
    expect(requestedURL).toBe('http://localhost:3000/api/v1/data/backup')
    expect(requestedURL).not.toContain('csrf')
    expect(requestedURL).not.toContain('token')
    expect(credentials).toBe('same-origin')
  })

  it('recovers from a backup failure without starting a duplicate download', async () => {
    useSettingsHandlers()
    let attempts = 0
    server.use(http.get('/api/v1/data/backup', () => {
      attempts += 1
      return attempts === 1
        ? HttpResponse.json({ error: { code: 'internal_error', message: 'failed' } }, { status: 500 })
        : new HttpResponse(new Uint8Array([83, 81, 76]), { headers: { 'Content-Type': 'application/vnd.sqlite3' } })
    }))
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:helio-backup')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    renderApp(<SettingsPage />)
    const backup = await screen.findByRole('button', { name: 'Baixar backup consistente' })
    await userEvent.click(backup)
    expect(await screen.findByText('Não foi possível preparar o backup. Tente novamente.')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Tentar baixar novamente' }))
    await waitFor(() => expect(attempts).toBe(2))
  })
})
