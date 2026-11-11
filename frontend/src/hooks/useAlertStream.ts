import { useEffect, useRef, useState } from 'react'
import { alertsSocketUrl } from '../lib/api'
import type { AlertMessage } from '../lib/types'

export type StreamStatus = 'connecting' | 'open' | 'reconnecting'

/**
 * Subscribes to /ws/alerts. Reconnects with exponential backoff (1s → 30s,
 * with jitter) and calls onReconnect after every successful *re*connect so the
 * caller can back-fill anything missed while offline.
 */
export function useAlertStream(onAlert: (a: AlertMessage) => void, onReconnect?: () => void) {
  const [status, setStatus] = useState<StreamStatus>('connecting')
  const onAlertRef = useRef(onAlert)
  const onReconnectRef = useRef(onReconnect)
  useEffect(() => {
    onAlertRef.current = onAlert
    onReconnectRef.current = onReconnect
  })

  useEffect(() => {
    let socket: WebSocket | null = null
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    let everOpened = false
    let disposed = false

    const connect = () => {
      socket = new WebSocket(alertsSocketUrl())
      socket.onopen = () => {
        if (everOpened) onReconnectRef.current?.()
        everOpened = true
        attempt = 0
        setStatus('open')
      }
      socket.onmessage = (ev) => {
        try {
          onAlertRef.current(JSON.parse(ev.data) as AlertMessage)
        } catch {
          /* ignore malformed frames */
        }
      }
      socket.onclose = () => {
        if (disposed) return
        setStatus('reconnecting')
        const delay = Math.min(30_000, 1000 * 2 ** attempt) * (0.75 + Math.random() * 0.5)
        attempt++
        retryTimer = setTimeout(connect, delay)
      }
      socket.onerror = () => socket?.close()
    }
    connect()

    return () => {
      disposed = true
      clearTimeout(retryTimer)
      socket?.close()
    }
  }, [])

  return status
}
