export function SessionUnavailable({ onRetry }: { onRetry: () => void }) {
  return (
    <main className="route-state">
      <div>
        <p>Não foi possível verificar sua sessão.</p>
        <button className="touch-target" onClick={onRetry} type="button">Tentar novamente</button>
      </div>
    </main>
  )
}
