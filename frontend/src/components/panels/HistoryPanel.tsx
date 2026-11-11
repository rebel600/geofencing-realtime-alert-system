import { ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import { useGeofences, useVehicles, useViolations } from '../../hooks/queries'
import { formatCoord, formatDateTime, localInputToISO } from '../../lib/format'
import { Button, Card, CategoryBadge, Empty, ErrorNote, EventBadge, Field, Input, LoadingRows, Section, Select } from '../ui'

const PAGE_SIZES = [10, 25, 50, 100]

export default function HistoryPanel() {
  const geofences = useGeofences()
  const vehicles = useVehicles()
  const [vehicleId, setVehicleId] = useState('')
  const [geofenceId, setGeofenceId] = useState('')
  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')
  const [pageSize, setPageSize] = useState(25)
  const [page, setPage] = useState(0)

  const startISO = localInputToISO(start)
  const endISO = localInputToISO(end)
  const rangeInvalid = !!startISO && !!endISO && endISO < startISO

  const q = useViolations({
    vehicle_id: vehicleId || undefined,
    geofence_id: geofenceId || undefined,
    start_date: rangeInvalid ? undefined : startISO,
    end_date: rangeInvalid ? undefined : endISO,
    limit: pageSize,
    offset: page * pageSize,
  })

  const total = q.data?.total_count ?? 0
  const pages = Math.max(1, Math.ceil(total / pageSize))
  const resetPage = <T,>(fn: (v: T) => void) => (v: T) => {
    fn(v)
    setPage(0)
  }
  const hasFilters = vehicleId || geofenceId || start || end

  return (
    <Section
      title="Violation history"
      action={
        hasFilters && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setVehicleId('')
              setGeofenceId('')
              setStart('')
              setEnd('')
              setPage(0)
            }}
          >
            <RotateCcw className="size-3.5" /> Reset
          </Button>
        )
      }
    >
      <div className="grid grid-cols-2 gap-2">
        <Field label="Vehicle" htmlFor="h-veh">
          <Select id="h-veh" value={vehicleId} onChange={(e) => resetPage(setVehicleId)(e.target.value)}>
            <option value="">All vehicles</option>
            {vehicles.data?.map((v) => (
              <option key={v.id} value={v.id}>
                {v.vehicle_number}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Geofence" htmlFor="h-geo">
          <Select id="h-geo" value={geofenceId} onChange={(e) => resetPage(setGeofenceId)(e.target.value)}>
            <option value="">All geofences</option>
            {geofences.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="From" htmlFor="h-start">
          <Input id="h-start" type="datetime-local" value={start} onChange={(e) => resetPage(setStart)(e.target.value)} />
        </Field>
        <Field label="To" htmlFor="h-end" error={rangeInvalid ? '“To” must be after “From”' : undefined}>
          <Input id="h-end" type="datetime-local" value={end} onChange={(e) => resetPage(setEnd)(e.target.value)} />
        </Field>
      </div>

      {q.isPending ? (
        <LoadingRows rows={5} />
      ) : q.isError ? (
        <ErrorNote error={q.error} onRetry={() => q.refetch()} />
      ) : q.data.violations.length === 0 ? (
        <Empty>{hasFilters ? 'No events match these filters.' : 'No entry or exit events recorded yet.'}</Empty>
      ) : (
        <ul className={q.isFetching ? 'flex flex-col gap-2 opacity-60 transition-opacity' : 'flex flex-col gap-2'}>
          {q.data.violations.map((v) => (
            <li key={v.id}>
              <Card className="py-2">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <EventBadge type={v.event_type} />
                  <span className="font-medium text-slate-900 dark:text-slate-100">{v.vehicle_number}</span>
                  <span className="text-slate-600 dark:text-slate-300">{v.geofence_name}</span>
                  <CategoryBadge category={v.category} />
                </div>
                <div className="mt-1 text-[11px] text-slate-500 dark:text-slate-400">
                  {formatDateTime(v.timestamp)} · <span className="font-mono">{formatCoord(v.latitude, v.longitude)}</span>
                </div>
              </Card>
            </li>
          ))}
        </ul>
      )}

      {total > 0 && (
        <div className="flex items-center justify-between gap-2 text-xs text-slate-600 dark:text-slate-400">
          <span>
            {page * pageSize + 1}–{Math.min(total, (page + 1) * pageSize)} of {total}
          </span>
          <div className="flex items-center gap-1">
            <Select aria-label="Rows per page" value={pageSize} onChange={(e) => resetPage(setPageSize)(Number(e.target.value))} className="h-8 w-20 text-xs">
              {PAGE_SIZES.map((n) => (
                <option key={n} value={n}>
                  {n} / page
                </option>
              ))}
            </Select>
            <Button variant="secondary" size="sm" disabled={page === 0} onClick={() => setPage((p) => p - 1)} aria-label="Previous page">
              <ChevronLeft className="size-3.5" />
            </Button>
            <span className="px-1">
              {page + 1} / {pages}
            </span>
            <Button variant="secondary" size="sm" disabled={page + 1 >= pages} onClick={() => setPage((p) => p + 1)} aria-label="Next page">
              <ChevronRight className="size-3.5" />
            </Button>
          </div>
        </div>
      )}
    </Section>
  )
}
