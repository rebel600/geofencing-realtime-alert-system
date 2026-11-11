import { MousePointerClick, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'
import { useUpdateLocation, useVehicles } from '../../hooks/queries'
import { ApiError } from '../../lib/api'
import { localInputToISO, toLocalInput } from '../../lib/format'
import type { LatLngPair, LocationUpdateResult } from '../../lib/types'
import { Button, Card, CategoryBadge, Empty, EventBadge, Field, Input, Section, Select } from '../ui'
import type { MapMode } from '../MapView'

interface Props {
  vehicleId: string | null
  setVehicleId: (id: string) => void
  mode: MapMode
  setMode: (m: MapMode) => void
  picked: LatLngPair | null
  setPicked: (p: LatLngPair | null) => void
}

export default function LocationPanel({ vehicleId, setVehicleId, mode, setMode, picked, setPicked }: Props) {
  const vehicles = useVehicles()
  const update = useUpdateLocation()
  const [timestamp, setTimestamp] = useState('')
  const [useNow, setUseNow] = useState(true)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [result, setResult] = useState<LocationUpdateResult | null>(null)

  const picking = mode === 'pick'
  const lat = picked?.[0]
  const lng = picked?.[1]

  const setCoord = (index: 0 | 1, value: string) => {
    const n = value === '' ? NaN : Number(value)
    const next: LatLngPair = [picked?.[0] ?? NaN, picked?.[1] ?? NaN]
    next[index] = n
    setPicked(Number.isNaN(next[0]) && Number.isNaN(next[1]) ? null : next)
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const next: Record<string, string> = {}
    if (!vehicleId) next.vehicle = 'Choose a vehicle'
    if (lat === undefined || Number.isNaN(lat) || lat < -90 || lat > 90) next.latitude = 'Latitude must be between -90 and 90'
    if (lng === undefined || Number.isNaN(lng) || lng < -180 || lng > 180) next.longitude = 'Longitude must be between -180 and 180'
    const iso = useNow ? new Date().toISOString() : localInputToISO(timestamp)
    if (!iso) next.timestamp = 'Pick a date and time'
    setErrors(next)
    if (Object.keys(next).length) return

    try {
      const res = await update.mutateAsync({ vehicle_id: vehicleId!, latitude: lat!, longitude: lng!, timestamp: iso! })
      setResult(res)
      if (res.stale) toast.warning('Location saved to history, but it is older than the last update, so zone status did not change.')
      else if (res.events.length === 0) toast.success('Location updated')
      setMode('idle')
    } catch (err) {
      if (err instanceof ApiError && err.field) setErrors({ [err.field]: err.message })
      else toast.error(err instanceof Error ? err.message : 'Could not update location')
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Section title="Update vehicle location">
        <form onSubmit={submit} className="flex flex-col gap-3" noValidate>
          <Field label="Vehicle" htmlFor="loc-vehicle" error={errors.vehicle}>
            <Select id="loc-vehicle" value={vehicleId ?? ''} onChange={(e) => setVehicleId(e.target.value)} disabled={vehicles.isPending}>
              <option value="" disabled>
                {vehicles.isPending ? 'Loading vehicles…' : 'Select a vehicle'}
              </option>
              {vehicles.data?.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.vehicle_number} — {v.driver_name}
                </option>
              ))}
            </Select>
          </Field>

          <div className="flex flex-col gap-2">
            <Button type="button" size="sm" variant={picking ? 'primary' : 'secondary'} onClick={() => setMode(picking ? 'idle' : 'pick')}>
              {picking ? <X className="size-3.5" /> : <MousePointerClick className="size-3.5" />}
              {picking ? 'Stop picking' : 'Pick point on map'}
            </Button>
            {picking && (
              <p className="rounded-md bg-sky-50 px-2.5 py-2 text-xs text-sky-900 dark:bg-sky-500/10 dark:text-sky-200">
                Click anywhere on the map to set the position. You can click again to adjust.
              </p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label="Latitude" htmlFor="loc-lat" error={errors.latitude}>
              <Input
                id="loc-lat"
                type="number"
                step="any"
                inputMode="decimal"
                value={lat === undefined || Number.isNaN(lat) ? '' : lat}
                onChange={(e) => setCoord(0, e.target.value)}
                placeholder="37.7799"
              />
            </Field>
            <Field label="Longitude" htmlFor="loc-lng" error={errors.longitude}>
              <Input
                id="loc-lng"
                type="number"
                step="any"
                inputMode="decimal"
                value={lng === undefined || Number.isNaN(lng) ? '' : lng}
                onChange={(e) => setCoord(1, e.target.value)}
                placeholder="-122.4144"
              />
            </Field>
          </div>

          <Field label="Timestamp" htmlFor="loc-ts" error={errors.timestamp}>
            <label className="flex items-center gap-2 text-xs text-slate-700 dark:text-slate-300">
              <input
                type="checkbox"
                checked={useNow}
                onChange={(e) => {
                  setUseNow(e.target.checked)
                  if (!e.target.checked && !timestamp) setTimestamp(toLocalInput(new Date()))
                }}
              />
              Use current time
            </label>
            {!useNow && <Input id="loc-ts" type="datetime-local" step="1" value={timestamp} onChange={(e) => setTimestamp(e.target.value)} />}
          </Field>

          <Button type="submit" loading={update.isPending}>
            Send location update
          </Button>
        </form>
      </Section>

      <Section title="Result">
        {!result ? (
          <Empty>Send an update to see which geofences the vehicle is in.</Empty>
        ) : (
          <Card className="flex flex-col gap-3">
            <div>
              <div className="text-xs font-medium text-slate-500 dark:text-slate-400">Currently inside</div>
              {result.current_geofences.length === 0 ? (
                <p className="text-sm text-slate-700 dark:text-slate-200">No geofences</p>
              ) : (
                <ul className="mt-1 flex flex-col gap-1">
                  {result.current_geofences.map((g) => (
                    <li key={g.geofence_id} className="flex items-center gap-2 text-sm text-slate-800 dark:text-slate-100">
                      {g.geofence_name} <CategoryBadge category={g.category} />
                    </li>
                  ))}
                </ul>
              )}
            </div>
            {result.events.length > 0 && (
              <div>
                <div className="text-xs font-medium text-slate-500 dark:text-slate-400">Transitions detected</div>
                <ul className="mt-1 flex flex-col gap-1">
                  {result.events.map((ev) => (
                    <li key={ev.event_id} className="flex items-center gap-2 text-sm text-slate-800 dark:text-slate-100">
                      <EventBadge type={ev.event_type} /> {ev.geofence.geofence_name}
                    </li>
                  ))}
                </ul>
                <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                  {result.alerts_triggered > 0
                    ? `${result.alerts_triggered} alert${result.alerts_triggered > 1 ? 's' : ''} sent to connected clients.`
                    : 'No alert rule matched these transitions.'}
                </p>
              </div>
            )}
            {result.stale && <p className="text-xs text-amber-700 dark:text-amber-400">Older than the last known position: saved to history only.</p>}
          </Card>
        )}
      </Section>
    </div>
  )
}
