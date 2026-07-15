import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import axe from 'axe-core'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { authMemory } from '../../api/client'
import { queryKeys } from '../../api/queries'
import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { LoginForm } from './LoginForm'

describe('LoginForm', () => {
  it.each(['light', 'dark'] as const)('não tem violações axe no tema %s', async (theme) => {
    localStorage.setItem('helio-theme', theme)
    const { container } = renderApp(<LoginForm />)
    const result = await axe.run(container, { rules: { 'color-contrast': { enabled: false } } })
    expect(result.violations).toEqual([])
  })

  it('avisa sobre HTTP local e recomenda uma senha exclusiva', () => {
    renderApp(<LoginForm />)

    expect(screen.getByText(/conexão local sem HTTPS/i)).toBeVisible()
    expect(screen.getByText(/não reutilize uma senha importante/i)).toBeVisible()
    expect(screen.getByLabelText('Senha')).toHaveAttribute('autocomplete', 'current-password')
  })

  it('mostra a contagem regressiva devolvida na sexta falha', async () => {
    const user = userEvent.setup()
    server.use(http.post('/api/v1/auth/login', () => HttpResponse.json(
      { error: { code: 'rate_limited', message: 'too many login attempts' } },
      { status: 429, headers: { 'Retry-After': '900' } },
    )))
    renderApp(<LoginForm />)

    await user.type(screen.getByLabelText('Usuário'), 'Admin')
    await user.type(screen.getByLabelText('Senha'), 'senha incorreta longa')
    await user.click(screen.getByRole('button', { name: 'Entrar no Helio' }))

    expect(await screen.findByRole('status')).toHaveTextContent(/15:00/)
    expect(screen.getByText(/Muitas tentativas/)).toHaveAttribute('aria-live', 'polite')
    expect(screen.getByRole('button', { name: /tente novamente em/i })).toBeDisabled()
  })

  it('diferencia falha do servidor de indisponibilidade de rede', async () => {
    const user = userEvent.setup()
    server.use(http.post('/api/v1/auth/login', () => HttpResponse.json(
      { error: { code: 'internal_error', message: 'login failed' } },
      { status: 500 },
    )))
    renderApp(<LoginForm />)
    await user.type(screen.getByLabelText('Senha'), 'senha segura local')
    await user.click(screen.getByRole('button', { name: 'Entrar no Helio' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/servidor/i)
  })

  it('hidrata sessão e CSRF apenas em memória após autenticar', async () => {
    const user = userEvent.setup()
    const onSuccess = vi.fn()
    server.use(http.post('/api/v1/auth/login', () => HttpResponse.json({
      userId: 'u1', username: 'Admin', expiresAt: '2026-08-14T00:00:00Z', csrfToken: 'fresh-login-token',
    })))
    const { client } = renderApp(<LoginForm onSuccess={onSuccess} />)

    await user.type(screen.getByLabelText('Usuário'), 'Admin')
    await user.type(screen.getByLabelText('Senha'), 'uma senha segura local')
    await user.click(screen.getByRole('button', { name: 'Entrar no Helio' }))

    await waitFor(() => expect(onSuccess).toHaveBeenCalledOnce())
    expect(authMemory.getCSRFToken()).toBe('fresh-login-token')
    expect(client.getQueryData(queryKeys.session)).toMatchObject({ username: 'Admin' })
    expect(localStorage.getItem('csrfToken')).toBeNull()
  })

  it('envia apenas um login quando o formulário dispara duas vezes no mesmo tick', async () => {
    let requests = 0
    let release: (() => void) | undefined
    const pending = new Promise<void>((resolve) => { release = resolve })
    server.use(http.post('/api/v1/auth/login', async () => {
      requests += 1
      await pending
      return HttpResponse.json({
        userId: 'u1', username: 'Admin', expiresAt: '2026-08-14T00:00:00Z', csrfToken: 'token',
      })
    }))
    renderApp(<LoginForm />)
    const form = screen.getByRole('button', { name: 'Entrar no Helio' }).closest('form')
    if (!form) throw new Error('login form missing')

    fireEvent.submit(form)
    fireEvent.submit(form)

    await waitFor(() => expect(requests).toBe(1))
    release?.()
  })
})
