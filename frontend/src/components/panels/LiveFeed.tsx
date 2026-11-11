import { BellOff, MapPin } from 'lucide-react'
import { useEffect, useState } from 'react'
import { formatCoord, formatDateTime, relativeTime } from '../../lib/format'
import type { AlertMessage } from '../../lib/types'
import { Button, Card, CategoryBadge, Empty, EventBadge, Section } from '../ui'

interface Props {
  alerts: AlertMessage[]
  loading: boolean
  onFocusVehicle: (id: string) => void
  onClear: () => void
}

export default function LiveFeed({ alerts, loading, onFocusVehicle, onClear }: Props) {
  // Re-render every 15s so relative times stay fresh.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 15_000)
    return () => clearInterval(id)
  }, [])

  return (
    <Section
      title="Live alerts"
      action={
        alerts.length > 0 && (
          <Button variant="ghost" size="sm" onClick={onClear}>
            <BellOff className="size-3.5" /> Clear
          </Button>
        )
      }
    >
      {loading && alerts.length === 0 ? (
        <Empty>Loading recent alerts…</Empty>
      ) : alerts.length === 0 ? (
        <Empty>
          No alerts yet. Alerts appear here instantly when a vehicle enters or exits a geofence that has an alert rule.
        </Empty>
      ) : (
        <ul className="flex flex-col gap-2" aria-live="polite">
          {alerts.map((a) => (
            <li key={a.event_id}>
              <Card
                className={
                  a.geofence.category === 'restricted_zone' && a.event_type === 'entry'
                    ? 'border-l-4 border-l-rose-500'
                    : undefined
                }
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <EventBadge type={a.event_type} />
                      <span className="text-sm font-medium text-slate-900 dark:text-slate-100">{a.vehicle.vehicle_number}</span>
                      <span className="text-xs text-slate-500">{a.vehicle.driver_name}</span>
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-1.5 text-sm text-slate-700 dark:text-slate-200">
                      {a.event_type === 'entry' ? 'entered' : 'left'} <strong>{a.geofence.geofence_name}</strong>
                      <CategoryBadge category={a.geofence.category} />
                    </div>
                    <div className="mt-1 text-[11px] text-slate-500 dark:text-slate-400">
                      <time dateTime={a.timestamp} title={formatDateTime(a.timestamp)}>
                        {relativeTime(a.timestamp, now)}
                      </time>{' '}
                      · <span className="font-mono">{formatCoord(a.location.latitude, a.location.longitude)}</span>
                    </div>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => onFocusVehicle(a.vehicle.vehicle_id)} aria-label="Show vehicle on map">
                    <MapPin className="size-3.5" />
                  </Button>
                </div>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}
