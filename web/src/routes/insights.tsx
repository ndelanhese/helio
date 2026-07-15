import { createFileRoute } from '@tanstack/react-router'

import { InsightsPage } from '../features/insights/InsightsPage'

export const Route = createFileRoute('/insights')({ component: InsightsPage })
