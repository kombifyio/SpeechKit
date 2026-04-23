const CONTROL_TOKEN_HEADER = 'X-SpeechKit-Control-Token'
const CONTROL_TOKEN_ENDPOINT = '/app/control-token'

let installed = false
let controlToken: string | null = null
let tokenRequest: Promise<string | null> | null = null

function isMutatingMethod(method: string): boolean {
  return ['POST', 'PUT', 'PATCH', 'DELETE'].includes(method.toUpperCase())
}

function requestMethod(input: RequestInfo | URL, init?: RequestInit): string {
  if (init?.method) return init.method
  if (input instanceof Request) return input.method
  return 'GET'
}

function isSameOriginRequest(input: RequestInfo | URL): boolean {
  if (typeof window === 'undefined') return false
  if (input instanceof Request) {
    return isSameOriginURL(input.url)
  }
  return isSameOriginURL(input.toString())
}

function isSameOriginURL(raw: string): boolean {
  if (typeof window === 'undefined') return false
  const url = new URL(raw, window.location.href)
  return url.origin === window.location.origin
}

function captureControlToken(response: Response): void {
  const nextToken = response.headers.get(CONTROL_TOKEN_HEADER)
  if (nextToken) {
    controlToken = nextToken
  }
}

async function ensureControlToken(
  nativeFetch: typeof fetch,
): Promise<string | null> {
  if (controlToken) return controlToken
  tokenRequest ??= nativeFetch(CONTROL_TOKEN_ENDPOINT, {
    cache: 'no-store',
    credentials: 'same-origin',
  })
    .then((response) => {
      captureControlToken(response)
      return controlToken
    })
    .catch(() => null)
    .finally(() => {
      tokenRequest = null
    })
  return tokenRequest
}

function withControlToken(
  input: RequestInfo | URL,
  init: RequestInit | undefined,
  token: string,
): RequestInit {
  const headers = new Headers(
    init?.headers ?? (input instanceof Request ? input.headers : undefined),
  )
  headers.set(CONTROL_TOKEN_HEADER, token)
  return {
    ...init,
    headers,
    credentials: init?.credentials ?? 'same-origin',
  }
}

export function installControlPlaneFetch(): void {
  if (installed || typeof window === 'undefined') return
  installed = true

  const nativeFetch = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    let nextInit = init
    if (
      isMutatingMethod(requestMethod(input, init)) &&
      isSameOriginRequest(input)
    ) {
      const token = await ensureControlToken(nativeFetch)
      if (token) {
        nextInit = withControlToken(input, init, token)
      }
    }

    const response = await nativeFetch(input, nextInit)
    if (isSameOriginRequest(input)) {
      captureControlToken(response)
    }
    return response
  }
}

installControlPlaneFetch()
