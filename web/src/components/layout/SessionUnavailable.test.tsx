import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { SessionUnavailable } from './SessionUnavailable'

describe('SessionUnavailable', () => {
  it('keeps the user in place and offers an explicit retry', async () => {
    const retry = vi.fn()
    render(<SessionUnavailable onRetry={retry} />)

    expect(screen.getByText('Não foi possível verificar sua sessão.')).toBeVisible()
    const button = screen.getByRole('button', { name: 'Tentar novamente' })
    expect(button).toHaveClass('touch-target')
    await userEvent.click(button)
    expect(retry).toHaveBeenCalledOnce()
  })
})
