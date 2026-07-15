import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, ArrowRight } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'

import { ApiError, authMemory } from '../../api/client'
import { bootstrapStatusQuery, createBootstrap, queryKeys } from '../../api/queries'
import {
  initialOnboardingValues,
  type FieldErrors,
  type OnboardingField,
  serverFieldError,
  toBootstrapPayload,
  validateStep,
} from './schema'
import {
  AccountStep,
  LocationStep,
  LoggerStep,
  OnboardingProgress,
  PanelsStep,
  ReviewStep,
  stepNames,
} from './steps'

const stepForField: Partial<Record<OnboardingField, number>> = {
  username: 0, password: 0, confirmPassword: 0,
  loggerHost: 1, loggerSerial: 1, loggerPort: 1, modbusSlave: 1,
  panelCount: 2, panelWattage: 2, activeMPPT: 2,
  latitude: 3, longitude: 3, timezone: 3, currency: 3, tariff: 3, retentionDays: 3,
}

export function OnboardingWizard({ onBootstrapClosed, onSuccess }: { onBootstrapClosed?: () => void; onSuccess?: () => void }) {
  const client = useQueryClient()
  const [step, setStep] = useState(0)
  const [values, setValues] = useState(initialOnboardingValues)
  const [errors, setErrors] = useState<FieldErrors>({})
  const [focusField, setFocusField] = useState<OnboardingField | null>(null)
  const [revealSerial, setRevealSerial] = useState(false)
  const submissionStarted = useRef(false)

  useLayoutEffect(() => {
    if (!focusField) return
    const target = document.getElementById(focusField) ?? document.querySelector(`[name="${focusField}"]`)
    if (target instanceof HTMLElement) target.focus()
    setFocusField(null)
  }, [focusField])

  const mutation = useMutation({
    mutationFn: createBootstrap,
    onSuccess: (session) => {
      authMemory.setCSRFToken(session.csrfToken)
      client.setQueryData(queryKeys.session, session)
      client.setQueryData(queryKeys.bootstrap, { open: false })
      onSuccess?.()
    },
    onError: async (error) => {
      submissionStarted.current = false
      if (error instanceof ApiError && error.status === 409) {
        try {
          await client.invalidateQueries({ queryKey: queryKeys.bootstrap, refetchType: 'all' })
          await client.fetchQuery(bootstrapStatusQuery)
        } catch {
          // The 409 is definitive; a transient status failure must not trap the user.
        } finally {
          onBootstrapClosed?.()
        }
        return
      }
      if (error instanceof ApiError && error.status === 422) {
        const [field, message] = serverFieldError(error.message, error.code)
        setErrors({ [field]: message })
        if (field !== 'general') {
          setStep(stepForField[field] ?? step)
          setFocusField(field)
        }
        return
      }
      setErrors({ general: 'Não foi possível concluir a configuração. Os dados foram mantidos; tente novamente.' })
    },
  })

  const update = (field: OnboardingField, value: string) => {
    setValues((current) => ({ ...current, [field]: value }))
    if (errors[field]) setErrors((current) => ({ ...current, [field]: undefined }))
  }
  const toggleMPPT = (input: number) => {
    setValues((current) => ({
      ...current,
      activeMPPT: current.activeMPPT.includes(input)
        ? current.activeMPPT.filter((item) => item !== input)
        : [...current.activeMPPT, input],
    }))
    setErrors((current) => ({ ...current, activeMPPT: undefined }))
  }
  const advance = () => {
    const found = validateStep(step, values)
    setErrors(found)
    const first = Object.keys(found)[0] as OnboardingField | undefined
    if (first) {
      setFocusField(first)
      return
    }
    setStep((current) => Math.min(current + 1, stepNames.length - 1))
  }
  const submit = () => {
    for (let candidate = 0; candidate < 4; candidate += 1) {
      const found = validateStep(candidate, values)
      const first = Object.keys(found)[0] as OnboardingField | undefined
      if (first) {
        setErrors(found)
        setStep(candidate)
        setFocusField(first)
        return
      }
    }
    setErrors({})
    if (submissionStarted.current) return
    submissionStarted.current = true
    mutation.mutate(toBootstrapPayload(values))
  }

  return (
    <div className="onboarding-wizard">
      <OnboardingProgress current={step} />
      <form noValidate onSubmit={(event) => {
        event.preventDefault()
        if (step === stepNames.length - 1) submit()
        else advance()
      }}>
        {step === 0 && <AccountStep errors={errors} update={update} values={values} />}
        {step === 1 && <LoggerStep errors={errors} onReveal={() => setRevealSerial((value) => !value)} revealSerial={revealSerial} update={update} values={values} />}
        {step === 2 && <PanelsStep errors={errors} toggleMPPT={toggleMPPT} update={update} values={values} />}
        {step === 3 && <LocationStep errors={errors} update={update} values={values} />}
        {step === 4 && <ReviewStep values={values} />}
        {errors.general && <p aria-live="assertive" className="form-alert" role="alert">{errors.general}</p>}
        <div className="form-actions">
          {step > 0 && <button className="secondary-action" disabled={mutation.isPending} onClick={() => { setErrors({}); setStep((current) => current - 1) }} type="button"><ArrowLeft aria-hidden="true" /> Voltar</button>}
          <button className="primary-action" disabled={mutation.isPending} type="submit">
            {mutation.isPending ? 'Criando seu Helio…' : buttonLabel(step)}
            {!mutation.isPending && step < 4 && <ArrowRight aria-hidden="true" />}
          </button>
        </div>
      </form>
    </div>
  )
}

function buttonLabel(step: number) {
  if (step === 0) return 'Continuar para o logger'
  if (step === 1) return 'Continuar para os painéis'
  if (step === 2) return 'Continuar para local e tarifa'
  if (step === 3) return 'Revisar configuração'
  return 'Criar Helio'
}
