import { Eye, EyeOff, LockKeyhole, SunMedium } from 'lucide-react'
import { Children, cloneElement, isValidElement, type InputHTMLAttributes, type ReactElement, type ReactNode } from 'react'

import { installedPower, type FieldErrors, type OnboardingField, type OnboardingValues } from './schema'

export const stepNames = ['Conta', 'Logger', 'Painéis', 'Local e tarifa', 'Revisão'] as const

interface SharedProps {
  errors: FieldErrors
  update: (field: OnboardingField, value: string) => void
  values: OnboardingValues
}

export function OnboardingProgress({ current }: { current: number }) {
  return (
    <nav aria-label="Progresso da configuração" className="onboarding-progress">
      <ol>
        {stepNames.map((name, index) => (
          <li aria-current={index === current ? 'step' : undefined} className={index < current ? 'is-complete' : undefined} key={name}>
            <span>{String(index + 1).padStart(2, '0')}</span>{name}
          </li>
        ))}
      </ol>
    </nav>
  )
}

export function AccountStep(props: SharedProps) {
  return (
    <StepHeading eyebrow="Primeiro acesso" title="Crie a conta local" text="Esta conta administra o monitor dentro da sua rede." >
      <Field field="username" label="Usuário administrador" error={props.errors.username}>
        <input autoComplete="username" id="username" name="username" onChange={(event) => props.update('username', event.target.value)} value={props.values.username} />
      </Field>
      <Field field="password" label="Senha" hint="De 12 a 128 caracteres. Guarde-a no seu gerenciador de senhas." error={props.errors.password}>
        <input autoComplete="new-password" id="password" name="password" onChange={(event) => props.update('password', event.target.value)} type="password" value={props.values.password} />
      </Field>
      <Field field="confirmPassword" label="Confirmar senha" error={props.errors.confirmPassword}>
        <input autoComplete="new-password" id="confirmPassword" name="confirmPassword" onChange={(event) => props.update('confirmPassword', event.target.value)} type="password" value={props.values.confirmPassword} />
      </Field>
      <p className="security-note"><LockKeyhole aria-hidden="true" /> Conexão local sem HTTPS: não reutilize uma senha importante.</p>
    </StepHeading>
  )
}

export function LoggerStep({ errors, update, values, revealSerial, onReveal }: SharedProps & { onReveal: () => void; revealSerial: boolean }) {
  return (
    <StepHeading eyebrow="Comunicação" title="Conecte o logger" text="Use o IPv4 que o roteador reservou para o inversor. O Helio confirma o alcance depois de salvar.">
      <Field field="loggerHost" label="Endereço IP do logger" hint="Prefira um IPv4 privado e reservado no roteador. Exemplo: 192.168.1.50" error={errors.loggerHost}>
        <input autoComplete="off" id="loggerHost" inputMode="decimal" name="loggerHost" onChange={(event) => update('loggerHost', event.target.value)} placeholder="192.168.1.50" value={values.loggerHost} />
      </Field>
      <Field field="loggerSerial" label="Número de série do logger" error={errors.loggerSerial}>
        <div className="secret-field">
          <input autoComplete="off" id="loggerSerial" inputMode="numeric" name="loggerSerial" onChange={(event) => update('loggerSerial', event.target.value)} type={revealSerial ? 'text' : 'password'} value={values.loggerSerial} />
          <button aria-label={revealSerial ? 'Ocultar número de série' : 'Mostrar número de série'} className="field-icon-button" onClick={onReveal} type="button">
            {revealSerial ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
          </button>
        </div>
      </Field>
      <div className="form-pair">
        <NumberField field="loggerPort" label="Porta" min="1" max="65535" {...{ errors, update, values }} />
        <NumberField field="modbusSlave" label="Endereço Modbus" min="1" max="247" {...{ errors, update, values }} />
      </div>
    </StepHeading>
  )
}

export function PanelsStep({ errors, update, values, toggleMPPT }: SharedProps & { toggleMPPT: (input: number) => void }) {
  const total = installedPower(values)
  return (
    <StepHeading eyebrow="Arranjo fotovoltaico" title="Descreva os painéis" text="A potência total é calculada automaticamente e usada para contextualizar a produção.">
      <div className="form-pair">
        <NumberField field="panelCount" label="Quantidade de painéis" min="1" {...{ errors, update, values }} />
        <NumberField field="panelWattage" label="Potência por painel (W)" min="1" {...{ errors, update, values }} />
      </div>
      <output className="capacity-output" htmlFor="panelCount panelWattage">
        <SunMedium aria-hidden="true" /><span>Potência instalada</span>
        <strong>{new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(total / 1000)} kWp</strong>
      </output>
      <fieldset aria-describedby={errors.activeMPPT ? 'activeMPPT-error' : undefined} className="mppt-fieldset">
        <legend>Entradas fotovoltaicas em uso</legend>
        {[1, 2].map((input) => (
          <label key={input}>
            <input aria-label={`Entrada PV${input} ativa`} checked={values.activeMPPT.includes(input)} id={`activeMPPT${input}`} name="activeMPPT" onChange={() => toggleMPPT(input)} type="checkbox" />
            <span>PV{input}</span>
          </label>
        ))}
        {errors.activeMPPT && <p className="field-error" id="activeMPPT-error">{errors.activeMPPT}</p>}
      </fieldset>
    </StepHeading>
  )
}

export function LocationStep(props: SharedProps) {
  return (
    <StepHeading eyebrow="Contexto" title="Local e tarifa" text="O local define o ciclo solar. A tarifa permite estimar o valor da energia gerada — não a economia da conta.">
      <div className="form-pair">
        <DecimalField field="latitude" label="Latitude" min="-90" max="90" {...props} />
        <DecimalField field="longitude" label="Longitude" min="-180" max="180" {...props} />
      </div>
      <Field field="timezone" label="Fuso horário IANA" hint="Exemplo: America/Sao_Paulo" error={props.errors.timezone}>
        <input autoComplete="off" id="timezone" name="timezone" onChange={(event) => props.update('timezone', event.target.value)} value={props.values.timezone} />
      </Field>
      <div className="form-pair">
        <Field field="currency" label="Moeda" error={props.errors.currency}>
          <input autoComplete="off" id="currency" maxLength={3} name="currency" onChange={(event) => props.update('currency', event.target.value.toUpperCase())} value={props.values.currency} />
        </Field>
        <DecimalField field="tariff" label="Tarifa por kWh" min="0" {...props} />
      </div>
      <NumberField field="retentionDays" label="Retenção do histórico (dias)" min="30" max="3650" {...props} />
    </StepHeading>
  )
}

export function ReviewStep({ values }: { values: OnboardingValues }) {
  const masked = values.loggerSerial.length > 4 ? `••••••${values.loggerSerial.slice(-4)}` : '••••••'
  return (
    <StepHeading eyebrow="Última conferência" title="Tudo pronto para começar" text="A criação da conta e das configurações acontece em uma única operação.">
      <section aria-label="Revisão da configuração" className="review-sheet">
        <dl>
          <ReviewLine label="Conta" value={values.username} />
          <ReviewLine label="Senha" value="••••••••••••" />
          <ReviewLine label="Logger" value={`${values.loggerHost}:${values.loggerPort} · série ${masked}`} />
          <ReviewLine label="Painéis" value={`${values.panelCount} × ${values.panelWattage} W · PV${values.activeMPPT.join(' + PV')}`} />
          <ReviewLine label="Local" value={`${values.latitude}, ${values.longitude} · ${values.timezone}`} />
          <ReviewLine label="Tarifa" value={`${values.currency} ${values.tariff}/kWh · ${values.retentionDays} dias`} />
        </dl>
      </section>
    </StepHeading>
  )
}

function StepHeading({ children, eyebrow, text, title }: { children: ReactNode; eyebrow: string; text: string; title: string }) {
  return <div className="onboarding-step"><p className="eyebrow">{eyebrow}</p><h1>{title}</h1><p className="step-intro">{text}</p><div className="form-fields">{children}</div></div>
}

function Field({ children, error, field, hint, label }: { children: ReactNode; error?: string; field: OnboardingField; hint?: string; label: string }) {
  const describedBy = [hint ? `${field}-hint` : '', error ? `${field}-error` : ''].filter(Boolean).join(' ') || undefined
  return (
    <div className={`form-field${error ? ' has-error' : ''}`}>
      <label htmlFor={field}>{label}</label>
      {hint && <p className="field-hint" id={`${field}-hint`}>{hint}</p>}
      <div>{describeInputs(children, describedBy, Boolean(error))}</div>
      {error && <p className="field-error" id={`${field}-error`}>{error}</p>}
    </div>
  )
}

function describeInputs(node: ReactNode, describedBy: string | undefined, invalid: boolean): ReactNode {
  if (!isValidElement(node)) return node
  if (node.type === 'input') {
    return cloneElement(node as ReactElement<InputHTMLAttributes<HTMLInputElement>>, {
      'aria-describedby': describedBy,
      'aria-invalid': invalid || undefined,
    })
  }
  const element = node as ReactElement<{ children?: ReactNode }>
  if (element.props.children === undefined) return node
  return cloneElement(element, {}, Children.map(element.props.children, (child) => describeInputs(child, describedBy, invalid)))
}

function NumberField({ errors, field, label, update, values, ...input }: SharedProps & { field: OnboardingField; label: string } & InputHTMLAttributes<HTMLInputElement>) {
  return <Field field={field} label={label} error={errors[field]}><input {...input} id={field} inputMode="numeric" name={field} onChange={(event) => update(field, event.target.value)} type="number" value={String(values[field])} /></Field>
}

function DecimalField({ errors, field, label, update, values, ...input }: SharedProps & { field: OnboardingField; label: string } & InputHTMLAttributes<HTMLInputElement>) {
  return <Field field={field} label={label} error={errors[field]}><input {...input} id={field} inputMode="decimal" name={field} onChange={(event) => update(field, event.target.value)} step="any" type="number" value={String(values[field])} /></Field>
}

function ReviewLine({ label, value }: { label: string; value: string }) {
  return <div><dt>{label}</dt><dd>{value}</dd></div>
}
