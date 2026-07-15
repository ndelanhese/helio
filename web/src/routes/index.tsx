import { createFileRoute } from '@tanstack/react-router'

import { NowPage } from '../features/live/NowPage'

export const Route = createFileRoute('/')({
  component: NowPage,
})
