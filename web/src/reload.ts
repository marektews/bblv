const KEY = 'bblv-reloaded'
const MIN_INTERVAL = 60_000

// reloadForUpdate reloads the page to get the frontend of an updated server.
// It reloads at most once a minute, so that a broken build cannot reload
// forever, and not at all without session storage. It returns whether it
// reloads.
export function reloadForUpdate(): boolean {
  try {
    const last = Number(sessionStorage.getItem(KEY) ?? 0)
    if (Date.now() - last < MIN_INTERVAL) {
      return false
    }
    sessionStorage.setItem(KEY, String(Date.now()))
  } catch {
    return false
  }
  window.location.reload()
  return true
}
