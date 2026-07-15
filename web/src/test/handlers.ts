import { http, HttpResponse } from 'msw'

import { authenticatedSession } from './fixtures'

export const handlers = [
  http.get('/api/v1/bootstrap/status', () => HttpResponse.json({ open: false })),
  http.get('/api/v1/auth/session', () => HttpResponse.json(authenticatedSession)),
]
