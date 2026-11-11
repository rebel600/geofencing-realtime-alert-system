import { useQueryClient } from '@tanstack/react-query'
import clsx from 'clsx'
import { Bell, Car, History, Hexagon, Navigation, Radio, ShieldAlert } from 'lucide-react'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import ErrorBoundary from './components/ErrorBoundary'
import type { MapFocus, MapMode } from './components/MapView'
import AlertRulesPanel from './components/panels/AlertRulesPanel'
import GeofencePanel from './components/panels/GeofencePanel'
import HistoryPanel from './components/panels/HistoryPanel'
import LiveFeed from './components/panels/LiveFeed'
import LocationPanel from './components/panels/LocationPanel'
import VehiclePanel from './components/panels/VehiclePanel'
import { keys, useGeofences, useVehicles } from './hooks/queries'
import { useAlertStream, type StreamStatus } from './hooks/useAlertStream'
import { api } from './lib/api'
import { formatTime } from './lib/format'
import type { AlertMessage, LatLngPair } from './lib/types'

const MapView = lazy(() => import('./components/MapView'))

const TABS = [
  { id: 'zones', label: 'Zones', icon: Hexagon },
  { id: 'vehicles', label: 'Vehicles', icon: Car },
  { id: 'track', label: 'Track', icon: Navigation },
  { id: 'rules', label: 'Rules', icon: ShieldAlert },
  { id: 'live', label: 'Live', icon: Bell },
  { id: 'history', label: 'History', icon: History },
] as const
type Tab = (typeof TABS)[number]['id']

const MAX_FEED = 100
const FLASH_MS = 8000

export default function App() {
  const qc = useQueryClient()
  const geofences = useGeofences()
  const vehicles = useVehicles()

  const [tab, setTabState] = useState<Tab>('zones')
  const [mapMode, setMapMode] = useState<MapMode>('idle')
  const [draft, setDraft] = useState<LatLngPair[] | null>(null)
  const [picked, setPicked] = useState<LatLngPair | null>(null)
  const [selectedVehicleId, setSelectedVehicleId] = useState<string | null>(null)
  const [focus, setFocus] = useState<MapFocus>(null)
  const [feed, setFeed] = useState<AlertMessage[]>([])
  const [feedLoading, setFeedLoading] = useState(true)
  const [unread, setUnread] = useState(0)
  const [alerting, setAlerting] = useState<Set<string>>(new Set())
  const flashTimers = useRef(new Map<string, ReturnType<typeof setTimeout>>())

  const setTab = (t: Tab) => {
    setTabState(t)
    if (t === 'live') setUnread(0)
    // Leaving a panel cancels its map interaction.
    if (t !== 'zones' && mapMode === 'draw') setMapMode('idle')
    if (t !== 'track' && mapMode === 'pick') setMapMode('idle')
  }

  const focusOn = (kind: 'geofence' | 'vehicle', id: string) => setFocus({ kind, id, nonce: Date.now() })

  const mergeAlerts = useCallback((incoming: AlertMessage[]) => {
    setFeed((prev) => {
      const seen = new Set(prev.map((a) => a.event_id))
      const fresh = incoming.filter((a) => !seen.has(a.event_id))
      if (!fresh.length) return prev
      return [...fresh, ...prev].sort((a, b) => b.timestamp.localeCompare(a.timestamp)).slice(0, MAX_FEED)
    })
  }, [])

  const backfill = useCallback(() => {
    api
      .recentAlerts(50)
      .then((r) => mergeAlerts(r.alerts))
      .catch(() => {
        /* live stream still works; feed just starts empty */
      })
      .finally(() => setFeedLoading(false))
  }, [mergeAlerts])

  useEffect(backfill, [backfill])

  const tabRef = useRef(tab)
  useEffect(() => {
    tabRef.current = tab
  })

  const onAlert = useCallback(
    (a: AlertMessage) => {
      mergeAlerts([a])
      if (tabRef.current !== 'live') setUnread((n) => n + 1)

      const restricted = a.geofence.category === 'restricted_zone' && a.event_type === 'entry'
      const verb = a.event_type === 'entry' ? 'entered' : 'left'
      const show = restricted ? toast.error : toast.info
      show(`${a.vehicle.vehicle_number} ${verb} ${a.geofence.geofence_name}`, {
        id: a.event_id,
        description: `${a.vehicle.driver_name} · ${a.geofence.category.replace('_', ' ')} · ${formatTime(a.timestamp)}`,
        duration: restricted ? 10_000 : 5_000,
        action: { label: 'Show', onClick: () => setFocus({ kind: 'vehicle', id: a.vehicle.vehicle_id, nonce: Date.now() }) },
      })

      // Flash the vehicle marker.
      const vid = a.vehicle.vehicle_id
      setAlerting((s) => new Set(s).add(vid))
      clearTimeout(flashTimers.current.get(vid))
      flashTimers.current.set(
        vid,
        setTimeout(() => {
          setAlerting((s) => {
            const next = new Set(s)
            next.delete(vid)
            return next
          })
        }, FLASH_MS),
      )

      qc.invalidateQueries({ queryKey: keys.vehicles })
      qc.invalidateQueries({ queryKey: keys.vehicleLocation(vid) })
      qc.invalidateQueries({ queryKey: keys.violations })
    },
    [mergeAlerts, qc],
  )

  const status = useAlertStream(onAlert, backfill)

  const pickedForMap = useMemo<LatLngPair | null>(
    () => (picked && Number.isFinite(picked[0]) && Number.isFinite(picked[1]) ? picked : null),
    [picked],
  )

  return (
    <div className="flex h-dvh flex-col bg-slate-50 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <header className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-slate-200 bg-white px-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="flex items-center gap-2.5">
          <img src="/favicon.svg" alt="" className="size-7" />
          <div className="leading-tight">
            <h1 className="text-sm font-semibold">Geofence Control</h1>
            <p className="hidden text-[11px] text-slate-500 sm:block dark:text-slate-400">Real-time vehicle geofencing & alerts</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <ConnectionBadge status={status} />
          <button
            type="button"
            onClick={() => setTab('live')}
            className="relative rounded-md p-2 text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
            aria-label={`Live alerts${unread ? `, ${unread} unread` : ''}`}
          >
            <Bell className="size-5" />
            {unread > 0 && (
              <span className="absolute -right-0.5 -top-0.5 min-w-4.5 rounded-full bg-rose-600 px-1 text-center text-[10px] font-semibold leading-4.5 text-white">
                {unread > 99 ? '99+' : unread}
              </span>
            )}
          </button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        <main className="relative h-[45vh] shrink-0 md:h-auto md:flex-1">
          <ErrorBoundary label="map">
          <Suspense fallback={<div className="h-full w-full animate-pulse bg-slate-200 dark:bg-slate-800" />}>
            <MapView
              geofences={geofences.data ?? []}
              vehicles={vehicles.data ?? []}
              mode={mapMode}
              draftPolygon={draft}
              pickedPoint={tab === 'track' ? pickedForMap : null}
              selectedVehicleId={selectedVehicleId}
              alertingVehicleIds={alerting}
              focus={focus}
              onPolygonDrawn={(ring) => {
                setDraft(ring)
                setMapMode('idle')
                setTab('zones')
              }}
              onPick={(p) => {
                setPicked(p)
                if (tab !== 'track') setTab('track')
              }}
              onSelectVehicle={(id) => {
                setSelectedVehicleId(id)
                setTab('vehicles')
              }}
            />
          </Suspense>
          </ErrorBoundary>
          <MapLegend />
          {(geofences.isError || vehicles.isError) && (
            <div className="absolute left-3 right-3 top-3 z-[1000] rounded-md bg-red-600 px-3 py-2 text-sm text-white shadow">
              {((geofences.error ?? vehicles.error) as Error).message}
            </div>
          )}
        </main>

        <aside className="flex min-h-0 flex-1 flex-col border-slate-200 bg-white md:w-[420px] md:flex-none md:border-l dark:border-slate-800 dark:bg-slate-900">
          <nav className="flex shrink-0 overflow-x-auto border-b border-slate-200 dark:border-slate-800" aria-label="Sections">
            {TABS.map(({ id, label, icon: Icon }) => (
              <button
                key={id}
                type="button"
                onClick={() => setTab(id)}
                aria-current={tab === id ? 'page' : undefined}
                className={clsx(
                  'relative flex flex-1 flex-col items-center gap-0.5 px-2 py-2 text-[11px] font-medium transition-colors',
                  tab === id
                    ? 'text-sky-700 after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-sky-600 dark:text-sky-400 dark:after:bg-sky-400'
                    : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200',
                )}
              >
                <Icon className="size-4" aria-hidden />
                {label}
                {id === 'live' && unread > 0 && <span className="absolute right-2 top-1.5 size-2 rounded-full bg-rose-600" />}
              </button>
            ))}
          </nav>

          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            {tab === 'zones' && (
              <GeofencePanel mode={mapMode} setMode={setMapMode} draft={draft} setDraft={setDraft} onFocus={(id) => focusOn('geofence', id)} />
            )}
            {tab === 'vehicles' && (
              <VehiclePanel
                selectedId={selectedVehicleId}
                onSelect={(id) => setSelectedVehicleId((cur) => (cur === id ? null : id))}
                onFocus={(id) => focusOn('vehicle', id)}
                onTrack={(id) => {
                  setSelectedVehicleId(id)
                  setTab('track')
                }}
              />
            )}
            {tab === 'track' && (
              <LocationPanel
                vehicleId={selectedVehicleId}
                setVehicleId={setSelectedVehicleId}
                mode={mapMode}
                setMode={setMapMode}
                picked={picked}
                setPicked={setPicked}
              />
            )}
            {tab === 'rules' && <AlertRulesPanel />}
            {tab === 'live' && (
              <LiveFeed alerts={feed} loading={feedLoading} onFocusVehicle={(id) => focusOn('vehicle', id)} onClear={() => setFeed([])} />
            )}
            {tab === 'history' && <HistoryPanel />}
          </div>
        </aside>
      </div>
    </div>
  )
}

function ConnectionBadge({ status }: { status: StreamStatus }) {
  const label = status === 'open' ? 'Live' : status === 'connecting' ? 'Connecting…' : 'Reconnecting…'
  return (
    <span
      className={clsx(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium',
        status === 'open'
          ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300'
          : 'bg-amber-50 text-amber-800 dark:bg-amber-500/10 dark:text-amber-300',
      )}
      role="status"
      title="Real-time alert connection"
    >
      <Radio className={clsx('size-3.5', status !== 'open' && 'animate-pulse')} aria-hidden />
      {label}
    </span>
  )
}

function MapLegend() {
  const items = [
    ['#2563eb', 'Delivery'],
    ['#dc2626', 'Restricted'],
    ['#d97706', 'Toll'],
    ['#059669', 'Customer'],
  ] as const
  return (
    <div className="pointer-events-none absolute bottom-3 left-3 z-[1000] hidden rounded-md bg-white/90 px-2.5 py-2 text-[11px] shadow-sm ring-1 ring-slate-200 sm:block dark:bg-slate-900/90 dark:ring-slate-700">
      <div className="flex flex-col gap-1">
        {items.map(([color, label]) => (
          <span key={label} className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-sm" style={{ background: color }} />
            {label}
          </span>
        ))}
        <span className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-full bg-slate-900 ring-2 ring-white dark:bg-slate-100" />
          Vehicle
        </span>
      </div>
    </div>
  )
}
