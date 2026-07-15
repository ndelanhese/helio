import '@testing-library/jest-dom/vitest'

import { afterAll, afterEach, beforeAll } from 'vitest'

import { server } from './server'

if (!window.matchMedia) {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: () => ({
      addEventListener: () => undefined,
      dispatchEvent: () => false,
      matches: false,
      media: '',
      onchange: null,
      removeEventListener: () => undefined,
    }),
  })
}

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  localStorage.clear()
  delete document.documentElement.dataset.theme
})
afterAll(() => server.close())
