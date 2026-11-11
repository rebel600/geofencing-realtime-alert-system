import clsx from 'clsx'
import { Crosshair, Navigation } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'
import { useCreateVehicle, useVehicleLocation, useVehicles } from '../../hooks/queries'
import { ApiError } from '../../lib/api'
import { formatCoord, relativeTime } from '../../lib/format'
import type { Vehicle } from '../../lib/types'
import { Button, Card, CategoryBadge, Empty, ErrorNote, Field, Input, LoadingRows, Section, Select } from '../ui'

const VEHICLE_TYPES = ['truck', 'van', 'car', 'motorcycle', 'bus']

interface Props {
  selectedId: string | null
  onSelect: (id: string) => void
  onFocus: (id: string) => void
  onTrack: (id: string) => void
}

export default function VehiclePanel({ selectedId, onSelect, onFocus, onTrack }: Props) {
  const list = useVehicles()
  const create = useCreateVehicle()
  const [form, setForm] = useState({ vehicle_number: '', driver_name: '', vehicle_type: 'truck', phone: '' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }))

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const next: Record<string, string> = {}
    if (!form.vehicle_number.trim()) next.vehicle_number = 'Vehicle number is required'
    if (!form.driver_name.trim()) next.driver_name = 'Driver name is required'
    if (!form.phone.trim()) next.phone = 'Phone is required'
    setErrors(next)
    if (Object.keys(next).length) return
    try {
      const res = await create.mutateAsync(form)
      toast.success(`Vehicle ${res.vehicle_number} registered`)
      setForm({ vehicle_number: '', driver_name: '', vehicle_type: form.vehicle_type, phone: '' })
      onSelect(res.id)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) setErrors({ vehicle_number: err.message })
      else if (err instanceof ApiError && err.field) setErrors({ [err.field]: err.message })
      else toast.error(err instanceof Error ? err.message : 'Could not register vehicle')
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Section title="Register vehicle">
        <form onSubmit={submit} className="grid grid-cols-2 gap-3" noValidate>
          <Field label="Vehicle number" htmlFor="v-num" error={errors.vehicle_number}>
            <Input id="v-num" value={form.vehicle_number} onChange={set('vehicle_number')} placeholder="KA-01-AB-1234" />
          </Field>
          <Field label="Type" htmlFor="v-type">
            <Select id="v-type" value={form.vehicle_type} onChange={set('vehicle_type')}>
              {VEHICLE_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t[0].toUpperCase() + t.slice(1)}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Driver name" htmlFor="v-driver" error={errors.driver_name}>
            <Input id="v-driver" value={form.driver_name} onChange={set('driver_name')} placeholder="John Doe" />
          </Field>
          <Field label="Phone" htmlFor="v-phone" error={errors.phone}>
            <Input id="v-phone" type="tel" value={form.phone} onChange={set('phone')} placeholder="+1234567890" />
          </Field>
          <Button type="submit" className="col-span-2" loading={create.isPending}>
            Register vehicle
          </Button>
        </form>
      </Section>

      <Section title={`Vehicles${list.data ? ` (${list.data.length})` : ''}`}>
        {list.isPending ? (
          <LoadingRows />
        ) : list.isError ? (
          <ErrorNote error={list.error} onRetry={() => list.refetch()} />
        ) : list.data.length === 0 ? (
          <Empty>No vehicles registered yet.</Empty>
        ) : (
          <ul className="flex flex-col gap-2">
            {list.data.map((v) => (
              <li key={v.id}>
                <VehicleRow
                  vehicle={v}
                  selected={v.id === selectedId}
                  onSelect={() => onSelect(v.id)}
                  onFocus={() => onFocus(v.id)}
                  onTrack={() => onTrack(v.id)}
                />
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  )
}

function VehicleRow({ vehicle: v, selected, onSelect, onFocus, onTrack }: {
  vehicle: Vehicle
  selected: boolean
  onSelect: () => void
  onFocus: () => void
  onTrack: () => void
}) {
  return (
    <Card className={clsx('p-0 transition-shadow', selected && 'ring-2 ring-sky-500')}>
      <button type="button" onClick={onSelect} className="flex w-full items-start justify-between gap-2 p-3 text-left">
        <div className="min-w-0">
          <div className="text-sm font-medium text-slate-900 dark:text-slate-100">{v.vehicle_number}</div>
          <div className="text-xs text-slate-600 dark:text-slate-400">
            {v.driver_name} · {v.vehicle_type} · {v.phone}
          </div>
        </div>
        <span className="shrink-0 text-[11px] text-slate-500 dark:text-slate-400">
          {v.current_location ? relativeTime(v.current_location.timestamp) : 'no location yet'}
        </span>
      </button>
      {selected && <VehicleStatus vehicleId={v.id} onFocus={onFocus} onTrack={onTrack} />}
    </Card>
  )
}

function VehicleStatus({ vehicleId, onFocus, onTrack }: { vehicleId: string; onFocus: () => void; onTrack: () => void }) {
  const q = useVehicleLocation(vehicleId)
  return (
    <div className="flex flex-col gap-2 border-t border-slate-200 px-3 py-2.5 dark:border-slate-700">
      {q.isPending ? (
        <div className="h-10 animate-pulse rounded bg-slate-100 dark:bg-slate-800" />
      ) : q.isError ? (
        <ErrorNote error={q.error} onRetry={() => q.refetch()} />
      ) : (
        <>
          <div className="text-xs text-slate-600 dark:text-slate-300">
            {q.data.current_location ? (
              <>
                Last seen at <span className="font-mono">{formatCoord(q.data.current_location.latitude, q.data.current_location.longitude)}</span>
              </>
            ) : (
              'This vehicle has not reported a location yet.'
            )}
          </div>
          {q.data.current_location && (
            <div className="flex flex-wrap items-center gap-1.5">
              {q.data.current_geofences.length === 0 ? (
                <span className="text-xs text-slate-500">Outside all geofences</span>
              ) : (
                q.data.current_geofences.map((g) => (
                  <span key={g.geofence_id} className="inline-flex items-center gap-1 text-xs text-slate-700 dark:text-slate-200">
                    Inside <strong>{g.geofence_name}</strong> <CategoryBadge category={g.category} />
                  </span>
                ))
              )}
            </div>
          )}
        </>
      )}
      <div className="flex gap-2">
        <Button size="sm" variant="secondary" onClick={onTrack}>
          <Navigation className="size-3.5" /> Update location
        </Button>
        {q.data?.current_location && (
          <Button size="sm" variant="ghost" onClick={onFocus}>
            <Crosshair className="size-3.5" /> Show on map
          </Button>
        )}
      </div>
    </div>
  )
}
