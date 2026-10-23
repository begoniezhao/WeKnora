export interface McpOAuthAuthorization {
  authorizationUrl: string
  authorizationAttempt: string
}

export interface McpOAuthPopupWindow {
  open: (url: string, target: string, features: string) => { closed: boolean; close: () => void } | null
  setInterval: (handler: () => void | Promise<void>, timeout: number) => number
  clearInterval: (id: number) => void
}

export type McpOAuthPopupResult =
  | { status: 'opened'; stop: () => void }
  | { status: 'inactive' }
  | { status: 'missing' }

const POPUP_NAME = 'mcp_oauth'
const POPUP_FEATURES = 'width=600,height=720'
const POLL_MS = 1500

/**
 * Opens the consent popup and polls until this authorization attempt succeeds
 * or the popup closes. The attempt id is sent on every poll so an older stored
 * token is not treated as fresh consent.
 *
 * stop() drops a late poll result. isActive() lets the caller ignore a result
 * after the drawer closes, the service changes, or a newer attempt starts.
 */
export async function openMcpOAuthPopup(options: {
  win: McpOAuthPopupWindow
  serviceId: string
  redirectUri: string
  frontendRedirect: string
  authorize: (
    serviceId: string,
    body: { redirect_uri: string; frontend_redirect: string },
  ) => Promise<McpOAuthAuthorization>
  pollStatus: (serviceId: string, authorizationAttempt: string) => Promise<boolean>
  isActive?: () => boolean
  onStatus?: (authorized: boolean) => void
  onAuthorized?: () => void | Promise<void>
  onClosed?: () => void
}): Promise<McpOAuthPopupResult> {
  const authorization = await options.authorize(options.serviceId, {
    redirect_uri: options.redirectUri,
    frontend_redirect: options.frontendRedirect,
  })
  if (options.isActive && !options.isActive()) return { status: 'inactive' }
  if (!authorization.authorizationUrl || !authorization.authorizationAttempt) return { status: 'missing' }

  const popup = options.win.open(authorization.authorizationUrl, POPUP_NAME, POPUP_FEATURES)
  let stopped = false
  let timer: number | null = null
  const stop = () => {
    if (stopped) return
    stopped = true
    if (timer !== null) {
      options.win.clearInterval(timer)
      timer = null
    }
  }
  const stale = () => stopped || (options.isActive ? !options.isActive() : false)

  timer = options.win.setInterval(async () => {
    if (stale()) return
    const closed = !popup || popup.closed
    let authorized: boolean | undefined
    try {
      authorized = await options.pollStatus(options.serviceId, authorization.authorizationAttempt)
    } catch {
      authorized = undefined
    }
    if (stale()) return
    if (authorized !== undefined) options.onStatus?.(authorized)
    if (!authorized && !closed) return
    stop()
    if (authorized) {
      try { popup?.close() } catch { /* cross-origin close may throw */ }
      await options.onAuthorized?.()
      return
    }
    options.onClosed?.()
  }, POLL_MS)

  return { status: 'opened', stop }
}
