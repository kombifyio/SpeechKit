import { afterEach, describe, expect, it, vi } from 'vitest'

const CONTROL_TOKEN_HEADER = 'X-SpeechKit-Control-Token'

const originalFetch = window.fetch

afterEach(() => {
  window.fetch = originalFetch
  vi.restoreAllMocks()
  vi.resetModules()
})

async function installWithNativeFetch(nativeFetch: typeof fetch): Promise<void> {
  window.fetch = nativeFetch
  vi.resetModules()
  await import('./control-plane-fetch')
}

function createFetchSpy(
  responder: (input: RequestInfo | URL, init?: RequestInit) => Response,
): ReturnType<typeof vi.fn> {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) =>
    responder(input, init),
  )
}

describe('control-plane fetch bootstrap', () => {
  it('adds the bootstrapped token header to same-origin mutating requests', async () => {
    const calls: Array<{ input: RequestInfo | URL; init?: RequestInit }> = []
    const nativeFetch = createFetchSpy((input, init) => {
      calls.push({ input, init })
      if (input === '/app/control-token') {
        return new Response(null, {
          status: 204,
          headers: { [CONTROL_TOKEN_HEADER]: 'token-123' },
        })
      }
      return new Response('{}', { status: 200 })
    }) as unknown as typeof fetch

    await installWithNativeFetch(nativeFetch)
    await window.fetch('/auth/logout', { method: 'POST' })

    expect(calls).toHaveLength(2)
    expect(calls[0].input).toBe('/app/control-token')
    expect(new Headers(calls[1].init?.headers).get(CONTROL_TOKEN_HEADER)).toBe(
      'token-123',
    )
    expect(calls[1].init?.credentials).toBe('same-origin')
  })

  it('reuses tokens captured from same-origin read responses', async () => {
    const calls: Array<{ input: RequestInfo | URL; init?: RequestInit }> = []
    const nativeFetch = createFetchSpy((input, init) => {
      calls.push({ input, init })
      if (input === '/app/version') {
        return new Response('{}', {
          status: 200,
          headers: { [CONTROL_TOKEN_HEADER]: 'token-from-read' },
        })
      }
      return new Response('{}', { status: 200 })
    }) as unknown as typeof fetch

    await installWithNativeFetch(nativeFetch)
    await window.fetch('/app/version')
    await window.fetch('/auth/logout', { method: 'POST' })

    expect(calls.map((call) => call.input)).not.toContain('/app/control-token')
    expect(new Headers(calls[1].init?.headers).get(CONTROL_TOKEN_HEADER)).toBe(
      'token-from-read',
    )
  })

  it('does not attach the token to cross-origin mutating requests', async () => {
    const calls: Array<{ input: RequestInfo | URL; init?: RequestInit }> = []
    const nativeFetch = createFetchSpy((input, init) => {
      calls.push({ input, init })
      return new Response('{}', { status: 200 })
    }) as unknown as typeof fetch

    await installWithNativeFetch(nativeFetch)
    await window.fetch('https://example.invalid/auth/logout', { method: 'POST' })

    expect(calls).toHaveLength(1)
    expect(new Headers(calls[0].init?.headers).get(CONTROL_TOKEN_HEADER)).toBe(
      null,
    )
  })
})
