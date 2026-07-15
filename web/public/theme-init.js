(() => {
  let choice = 'system'
  try {
    const stored = localStorage.getItem('helio.theme.v1')
    if (stored === 'light' || stored === 'dark' || stored === 'system') choice = stored
  } catch {
    choice = 'system'
  }
  let dark = false
  try {
    dark = window.matchMedia('(prefers-color-scheme: dark)').matches
  } catch {
    dark = false
  }
  const resolved = choice === 'system' ? (dark ? 'dark' : 'light') : choice
  document.documentElement.dataset.theme = resolved
  document.documentElement.style.colorScheme = resolved
})()
