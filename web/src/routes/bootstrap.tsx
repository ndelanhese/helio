import { createFileRoute } from '@tanstack/react-router'

import { OnboardingWizard } from '../features/onboarding/OnboardingWizard'
import { AccessPage } from './login'

export const Route = createFileRoute('/bootstrap')({
  component: BootstrapRoute,
})

function BootstrapRoute() {
  return (
    <AccessPage kicker="Comece pela luz" note="Cinco passos para transformar sinais do inversor em uma leitura clara da sua casa.">
      <OnboardingWizard onBootstrapClosed={() => window.location.assign('/login')} onSuccess={() => window.location.assign('/')} />
    </AccessPage>
  )
}
