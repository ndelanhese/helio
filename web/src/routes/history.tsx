import { createFileRoute } from '@tanstack/react-router'

import { HistoryPage } from '../features/history/HistoryPage'

export const Route = createFileRoute('/history')({
  component: HistoryPage,
})
