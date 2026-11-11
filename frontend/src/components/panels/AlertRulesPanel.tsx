import clsx from 'clsx'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'
import { useAlertRules, useConfigureAlert, useDeactivateAlert, useGeofences, useVehicles } from '../../hooks/queries'
import { formatDateTime } from '../../lib/format'
import type { RuleEventType } from '../../lib/types'
import { Button, Card, Empty, ErrorNote, EventBadge, Field, LoadingRows, Section, Select } from '../ui'

export default function AlertRulesPanel() {
  const geofences = useGeofences()
  const vehicles = useVehicles()
  const configure = useConfigureAlert()
  const deactivate = useDeactivateAlert()

  const [geofenceId, setGeofenceId] = useState('')
  const [vehicleId, setVehicleId] = useState('')
  const [eventType, setEventType] = useState<RuleEventType>('entry')
  const [error, setError] = useState('')

  const [filterGeofence, setFilterGeofence] = useState('')
  const [filterVehicle, setFilterVehicle] = useState('')
  const rules = useAlertRules({ geofence_id: filterGeofence || undefined, vehicle_id: filterVehicle || undefined })

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!geofenceId) {
      setError('Choose a geofence')
      return
    }
    setError('')
    try {
      const rule = await configure.mutateAsync({ geofence_id: geofenceId, vehicle_id: vehicleId || undefined, event_type: eventType })
      toast.success(`Alert rule created for ${rule.geofence_name}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Could not create rule')
    }
  }

  const onDeactivate = async (id: string) => {
    try {
      await deactivate.mutateAsync(id)
      toast.success('Alert rule turned off')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Could not turn off rule')
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Section title="Configure alert">
        <form onSubmit={submit} className="flex flex-col gap-3" noValidate>
          <Field label="Geofence" htmlFor="ar-geo" error={error}>
            <Select id="ar-geo" value={geofenceId} onChange={(e) => setGeofenceId(e.target.value)}>
              <option value="">{geofences.isPending ? 'Loading…' : 'Select a geofence'}</option>
              {geofences.data?.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </Select>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Vehicle" htmlFor="ar-veh" hint="Leave as “All vehicles” to watch every vehicle.">
              <Select id="ar-veh" value={vehicleId} onChange={(e) => setVehicleId(e.target.value)}>
                <option value="">All vehicles</option>
                {vehicles.data?.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.vehicle_number}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Trigger on" htmlFor="ar-ev">
              <Select id="ar-ev" value={eventType} onChange={(e) => setEventType(e.target.value as RuleEventType)}>
                <option value="entry">Entry</option>
                <option value="exit">Exit</option>
                <option value="both">Entry and exit</option>
              </Select>
            </Field>
          </div>
          <Button type="submit" loading={configure.isPending}>
            Create alert rule
          </Button>
        </form>
      </Section>

      <Section title="Alert rules">
        <div className="grid grid-cols-2 gap-2">
          <Select aria-label="Filter by geofence" value={filterGeofence} onChange={(e) => setFilterGeofence(e.target.value)} className="h-8 text-xs">
            <option value="">Any geofence</option>
            {geofences.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
          <Select aria-label="Filter by vehicle" value={filterVehicle} onChange={(e) => setFilterVehicle(e.target.value)} className="h-8 text-xs">
            <option value="">Any vehicle</option>
            {vehicles.data?.map((v) => (
              <option key={v.id} value={v.id}>
                {v.vehicle_number}
              </option>
            ))}
          </Select>
        </div>
        {rules.isPending ? (
          <LoadingRows />
        ) : rules.isError ? (
          <ErrorNote error={rules.error} onRetry={() => rules.refetch()} />
        ) : rules.data.length === 0 ? (
          <Empty>No alert rules yet. Alerts only fire for geofences that have a rule.</Empty>
        ) : (
          <ul className="flex flex-col gap-2">
            {rules.data.map((r) => (
              <li key={r.alert_id}>
                <Card className={clsx('flex items-start justify-between gap-2', r.status !== 'active' && 'opacity-60')}>
                  <div className="min-w-0 text-sm">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-slate-900 dark:text-slate-100">{r.geofence_name}</span>
                      <EventBadge type={r.event_type} />
                      {r.status !== 'active' && <span className="text-[11px] font-medium uppercase text-slate-500">off</span>}
                    </div>
                    <div className="text-xs text-slate-600 dark:text-slate-400">{r.vehicle_number ?? 'All vehicles'}</div>
                    <div className="mt-1 font-mono text-[11px] text-slate-400">
                      {r.alert_id} · {formatDateTime(r.created_at)}
                    </div>
                  </div>
                  {r.status === 'active' && (
                    <Button
                      variant="danger"
                      size="sm"
                      loading={deactivate.isPending && deactivate.variables === r.alert_id}
                      onClick={() => onDeactivate(r.alert_id)}
                    >
                      Turn off
                    </Button>
                  )}
                </Card>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  )
}
