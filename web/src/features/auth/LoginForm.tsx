import { useMutation, useQueryClient } from '@tanstack/react-query'
import { LockKeyhole } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { ApiError, authMemory } from '../../api/client'
import { login, queryKeys } from '../../api/queries'

export function LoginForm({ onSuccess }: { onSuccess?: () => void }) {
  const client = useQueryClient()
  const [username, setUsername] = useState('Admin')
  const [password, setPassword] = useState('')
  const [remaining, setRemaining] = useState(0)
  const [message, setMessage] = useState('')
  const submissionStarted = useRef(false)
  const mutation = useMutation({
    mutationFn: login,
    onSuccess: (session) => {
      authMemory.setCSRFToken(session.csrfToken)
      client.setQueryData(queryKeys.session, session)
      onSuccess?.()
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 429) {
        setRemaining(error.retryAfterSeconds ?? 60)
        return
      }
      if (error instanceof ApiError && error.status === 401) {
        setMessage('Usuário ou senha não conferem. Verifique os dados e tente novamente.')
      } else if (error instanceof ApiError && error.status >= 500) {
        setMessage('O servidor Helio não conseguiu concluir o login. Aguarde um instante e tente novamente.')
      } else {
        setMessage('Não foi possível alcançar o Helio. Verifique a conexão de rede e tente novamente.')
      }
    },
    onSettled: () => { submissionStarted.current = false },
  })

  useEffect(() => {
    if (remaining <= 0) return
    const timer = window.setInterval(() => setRemaining((value) => Math.max(0, value - 1)), 1000)
    return () => window.clearInterval(timer)
  }, [remaining])

  const countdown = `${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, '0')}`

  return (
    <form className="login-form" onSubmit={(event) => {
      event.preventDefault()
      setMessage('')
      if (submissionStarted.current || remaining > 0) return
      submissionStarted.current = true
      mutation.mutate({ username: username.trim(), password })
    }}>
      <div className="onboarding-step">
        <p className="eyebrow">Área protegida</p>
        <h1>Entre no seu Helio</h1>
        <p className="step-intro">Sua produção solar continua privada, dentro da sua rede.</p>
      </div>
      <div className="form-fields">
        <div className="form-field"><label htmlFor="login-username">Usuário</label><div><input autoComplete="username" id="login-username" onChange={(event) => setUsername(event.target.value)} value={username} /></div></div>
        <div className="form-field"><label htmlFor="login-password">Senha</label><div><input autoComplete="current-password" id="login-password" onChange={(event) => setPassword(event.target.value)} required type="password" value={password} /></div></div>
        <p className="security-note"><LockKeyhole aria-hidden="true" /> Conexão local sem HTTPS: não reutilize uma senha importante.</p>
        {remaining > 0 && <>
          <p aria-live="polite" className="sr-only" role="status">Muitas tentativas. Aguarde antes de tentar novamente.</p>
          <p aria-hidden="true" className="form-alert">Muitas tentativas. Tente novamente em {countdown}.</p>
        </>}
        {message && <p aria-live="assertive" className="form-alert" role="alert">{message}</p>}
        <button className="primary-action" disabled={mutation.isPending || remaining > 0} type="submit">
          {mutation.isPending ? 'Verificando acesso…' : remaining > 0 ? `Tente novamente em ${countdown}` : 'Entrar no Helio'}
        </button>
      </div>
    </form>
  )
}
