import type {
  AlertMessage,
  AlertRule,
  Category,
  Geofence,
  LatLngPair,
  LocationUpdateResult,
  RuleEventType,
  Vehicle,
  VehicleLocation,
  Violation,
} from './types'

export const API_URL = (import.meta.env.VITE_API_URL ?? 'http://localhost:8080').replace(/\/+$/, '')
const API_KEY: string = import.meta.env.VITE_API_KEY ?? ''

/** WebSocket URL derived from the API base (http→ws, https→wss). */
export function alertsSocketUrl(): string {
  const url = new URL(API_URL + '/ws/alerts')
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  if (API_KEY) url.searchParams.set('api_key', API_KEY)
  return url.toString()
}

export class ApiError extends Error {
  readonly status: number
  readonly field?: string
  constructor(status: number, message: string, field?: string) {
    super(message)
    this.status = status
    this.field = field
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T & { time_ns: string }> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (API_KEY) headers['X-API-Key'] = API_KEY

  let res: Response
  try {
    res = await fetch(API_URL + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
  } catch {
    throw new ApiError(0, `Can't reach the server at ${API_URL}. Is the backend running?`)
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const message = data?.message ?? data?.error ?? `Request failed (${res.status})`
    throw new ApiError(res.status, message, data?.field)
  }
  return data
}

function qs(params: Record<string, string | number | undefined>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') sp.set(k, String(v))
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export const api = {
  listGeofences: (category?: Category | '') =>
    request<{ geofences: Geofence[]; total_count: number }>('GET', '/geofences' + qs({ category })),
  createGeofence: (input: { name: string; description: string; category: Category; coordinates: LatLngPair[] }) =>
    request<{ id: string; name: string; status: string }>('POST', '/geofences', input),

  listVehicles: () => request<{ vehicles: Vehicle[]; total_count: number }>('GET', '/vehicles'),
  createVehicle: (input: { vehicle_number: string; driver_name: string; vehicle_type: string; phone: string }) =>
    request<{ id: string; vehicle_number: string; status: string }>('POST', '/vehicles', input),

  updateLocation: (input: { vehicle_id: string; latitude: number; longitude: number; timestamp: string }) =>
    request<LocationUpdateResult>('POST', '/vehicles/location', input),
  vehicleLocation: (vehicleId: string) =>
    request<VehicleLocation>('GET', `/vehicles/location/${encodeURIComponent(vehicleId)}`),

  configureAlert: (input: { geofence_id: string; vehicle_id?: string; event_type: RuleEventType }) =>
    request<AlertRule>('POST', '/alerts/configure', input),
  listAlertRules: (filters: { geofence_id?: string; vehicle_id?: string }) =>
    request<{ alerts: AlertRule[]; total_count: number }>('GET', '/alerts' + qs(filters)),
  deactivateAlertRule: (alertId: string) =>
    request<{ alert_id: string; status: string }>('DELETE', `/alerts/${encodeURIComponent(alertId)}`),
  recentAlerts: (limit = 50) => request<{ alerts: AlertMessage[] }>('GET', '/alerts/recent' + qs({ limit })),

  violations: (filters: {
    vehicle_id?: string
    geofence_id?: string
    start_date?: string
    end_date?: string
    limit?: number
    offset?: number
  }) => request<{ violations: Violation[]; total_count: number }>('GET', '/violations/history' + qs(filters)),
}
