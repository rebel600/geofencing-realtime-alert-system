import '../lib/leafletGlobal'
import '@geoman-io/leaflet-geoman-free'
import '@geoman-io/leaflet-geoman-free/dist/leaflet-geoman.css'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { useEffect, useRef } from 'react'
import { CircleMarker, MapContainer, Polygon, TileLayer, Tooltip, useMap, useMapEvents } from 'react-leaflet'
import { formatCoord, relativeTime } from '../lib/format'
import { CATEGORY_META, type Geofence, type LatLngPair, type Vehicle } from '../lib/types'

export type MapMode = 'idle' | 'draw' | 'pick'
export type MapFocus = { kind: 'geofence' | 'vehicle'; id: string; nonce: number } | null

interface Props {
  geofences: Geofence[]
  vehicles: Vehicle[]
  mode: MapMode
  draftPolygon: LatLngPair[] | null
  pickedPoint: LatLngPair | null
  selectedVehicleId: string | null
  alertingVehicleIds: Set<string>
  focus: MapFocus
  onPolygonDrawn: (ring: LatLngPair[]) => void
  onPick: (point: LatLngPair) => void
  onSelectVehicle: (id: string) => void
}

const DEFAULT_CENTER: LatLngPair = [37.7799, -122.4144] // San Francisco (matches the spec examples)
const round = (n: number) => Math.round(n * 1e6) / 1e6

export default function MapView(props: Props) {
  const { geofences, vehicles, draftPolygon, pickedPoint, selectedVehicleId, alertingVehicleIds, onSelectVehicle } = props

  return (
    <MapContainer center={DEFAULT_CENTER} zoom={13} className="h-full w-full" zoomControl={false} worldCopyJump>
      <TileLayer
        attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
        url="https://tile.openstreetmap.org/{z}/{x}/{y}.png"
        maxZoom={19}
      />
      <ZoomControlBottomRight />
      <FitOnFirstData geofences={geofences} vehicles={vehicles} />
      <FlyToFocus focus={props.focus} geofences={geofences} vehicles={vehicles} />
      <DrawController mode={props.mode} onPolygonDrawn={props.onPolygonDrawn} />
      <PickController mode={props.mode} onPick={props.onPick} />

      {geofences.map((g) => {
        const color = CATEGORY_META[g.category]?.color ?? '#475569'
        return (
          <Polygon
            key={g.id}
            positions={g.coordinates}
            pathOptions={{
              color,
              weight: 2,
              fillOpacity: g.category === 'restricted_zone' ? 0.22 : 0.12,
              dashArray: g.category === 'restricted_zone' ? '6 4' : undefined,
            }}
            // Path clicks bubble to the map, so pick mode also works inside a zone.
          >
            <Tooltip sticky>
              <div className="text-xs">
                <div className="font-semibold">{g.name}</div>
                <div>{CATEGORY_META[g.category]?.label}</div>
              </div>
            </Tooltip>
          </Polygon>
        )
      })}

      {draftPolygon && (
        <Polygon positions={draftPolygon} pathOptions={{ color: '#0ea5e9', weight: 2, dashArray: '4 4', fillOpacity: 0.15 }} />
      )}

      {vehicles.map((v) => {
        if (!v.current_location) return null
        const alerting = alertingVehicleIds.has(v.id)
        const selected = v.id === selectedVehicleId
        return (
          <CircleMarker
            key={v.id}
            center={[v.current_location.latitude, v.current_location.longitude]}
            radius={selected ? 10 : 8}
            pathOptions={{
              color: '#ffffff',
              weight: 2.5,
              fillColor: alerting ? '#e11d48' : selected ? '#0284c7' : '#0f172a',
              fillOpacity: 1,
              className: alerting ? 'vehicle-alerting' : undefined,
            }}
            eventHandlers={{ click: () => onSelectVehicle(v.id) }}
          >
            <Tooltip direction="top" offset={[0, -8]} permanent={selected || alerting}>
              <div className="text-xs">
                <div className="font-semibold">{v.vehicle_number}</div>
                <div>{v.driver_name}</div>
                <div className="text-slate-500">updated {relativeTime(v.current_location.timestamp)}</div>
              </div>
            </Tooltip>
          </CircleMarker>
        )
      })}

      {pickedPoint && (
        <CircleMarker center={pickedPoint} radius={7} pathOptions={{ color: '#0284c7', weight: 3, fillColor: '#38bdf8', fillOpacity: 0.9 }}>
          <Tooltip direction="top" offset={[0, -6]} permanent>
            <span className="text-xs">New location · {formatCoord(pickedPoint[0], pickedPoint[1])}</span>
          </Tooltip>
        </CircleMarker>
      )}
    </MapContainer>
  )
}

function ZoomControlBottomRight() {
  const map = useMap()
  useEffect(() => {
    const ctl = L.control.zoom({ position: 'bottomright' }).addTo(map)
    return () => {
      ctl.remove()
    }
  }, [map])
  return null
}

function FitOnFirstData({ geofences, vehicles }: { geofences: Geofence[]; vehicles: Vehicle[] }) {
  const map = useMap()
  const done = useRef(false)
  useEffect(() => {
    if (done.current) return
    const points: L.LatLngExpression[] = [
      ...geofences.flatMap((g) => g.coordinates),
      ...vehicles.flatMap((v) => (v.current_location ? [[v.current_location.latitude, v.current_location.longitude] as LatLngPair] : [])),
    ]
    if (points.length === 0) return
    done.current = true
    map.fitBounds(L.latLngBounds(points), { padding: [40, 40], maxZoom: 15 })
  }, [geofences, vehicles, map])
  return null
}

function FlyToFocus({ focus, geofences, vehicles }: { focus: MapFocus; geofences: Geofence[]; vehicles: Vehicle[] }) {
  const map = useMap()
  useEffect(() => {
    if (!focus) return
    if (focus.kind === 'geofence') {
      const g = geofences.find((x) => x.id === focus.id)
      if (g) map.flyToBounds(L.latLngBounds(g.coordinates), { padding: [60, 60], maxZoom: 16, duration: 0.6 })
    } else {
      const loc = vehicles.find((x) => x.id === focus.id)?.current_location
      if (loc) map.flyTo([loc.latitude, loc.longitude], Math.max(map.getZoom(), 15), { duration: 0.6 })
    }
    // Only react to new focus requests, not to data refreshes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focus?.nonce])
  return null
}

/** Enables Geoman polygon drawing in 'draw' mode and reports the closed ring. */
function DrawController({ mode, onPolygonDrawn }: { mode: MapMode; onPolygonDrawn: (ring: LatLngPair[]) => void }) {
  const map = useMap()
  const cb = useRef(onPolygonDrawn)
  useEffect(() => {
    cb.current = onPolygonDrawn
  })

  useEffect(() => {
    if (mode !== 'draw') return
    const onCreate = (e: { layer: L.Layer }) => {
      const layer = e.layer as L.Polygon
      const latlngs = layer.getLatLngs()[0] as L.LatLng[]
      map.removeLayer(layer) // the draft is rendered by React from state
      const ring: LatLngPair[] = latlngs.map((p) => [round(p.lat), round(L.Util.wrapNum(p.lng, [-180, 180], true))])
      if (ring.length >= 3) cb.current([...ring, ring[0]]) // API requires a closed ring
    }
    map.on('pm:create', onCreate)
    map.pm.enableDraw('Polygon', { snappable: true, allowSelfIntersection: false, continueDrawing: false })
    return () => {
      map.off('pm:create', onCreate)
      map.pm.disableDraw()
    }
  }, [mode, map])
  return null
}

function PickController({ mode, onPick }: { mode: MapMode; onPick: (p: LatLngPair) => void }) {
  const map = useMapEvents({
    click(e) {
      if (mode === 'pick') onPick([round(e.latlng.lat), round(L.Util.wrapNum(e.latlng.lng, [-180, 180], true))])
    },
  })
  useEffect(() => {
    map.getContainer().style.cursor = mode === 'pick' ? 'crosshair' : ''
  }, [mode, map])
  return null
}
