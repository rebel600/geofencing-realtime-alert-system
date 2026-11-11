export const CATEGORIES = ['delivery_zone', 'restricted_zone', 'toll_zone', 'customer_area'] as const
export type Category = (typeof CATEGORIES)[number]

export const CATEGORY_META: Record<Category, { label: string; color: string; badge: string }> = {
  delivery_zone: { label: 'Delivery zone', color: '#2563eb', badge: 'bg-blue-100 text-blue-800 dark:bg-blue-500/15 dark:text-blue-300' },
  restricted_zone: { label: 'Restricted zone', color: '#dc2626', badge: 'bg-red-100 text-red-800 dark:bg-red-500/15 dark:text-red-300' },
  toll_zone: { label: 'Toll zone', color: '#d97706', badge: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-300' },
  customer_area: { label: 'Customer area', color: '#059669', badge: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-300' },
}

export type LatLngPair = [number, number]

export interface Geofence {
  id: string
  name: string
  description: string
  coordinates: LatLngPair[]
  category: Category
  status: string
  created_at: string
}

export interface Location {
  latitude: number
  longitude: number
  timestamp: string
}

export interface Vehicle {
  id: string
  vehicle_number: string
  driver_name: string
  vehicle_type: string
  phone: string
  status: string
  created_at: string
  current_location: Location | null
}

export interface GeofenceRef {
  geofence_id: string
  geofence_name: string
  category: Category
}

export interface VehicleLocation {
  vehicle_id: string
  vehicle_number: string
  current_location: Location | null
  current_geofences: GeofenceRef[]
}

export interface LocationUpdateResult {
  vehicle_id: string
  location_updated: boolean
  current_geofences: (GeofenceRef & { status: string })[]
  events: { event_id: string; event_type: EventType; geofence: GeofenceRef }[]
  alerts_triggered: number
  stale: boolean
}

export type EventType = 'entry' | 'exit'
export type RuleEventType = EventType | 'both'

export interface AlertRule {
  alert_id: string
  geofence_id: string
  geofence_name: string
  vehicle_id: string | null
  vehicle_number: string | null
  event_type: RuleEventType
  status: 'active' | 'inactive'
  created_at: string
}

export interface Violation {
  id: string
  vehicle_id: string
  vehicle_number: string
  geofence_id: string
  geofence_name: string
  category: Category
  event_type: EventType
  latitude: number
  longitude: number
  timestamp: string
}

/** Real-time alert message pushed over /ws/alerts. */
export interface AlertMessage {
  event_id: string
  event_type: EventType
  timestamp: string
  vehicle: { vehicle_id: string; vehicle_number: string; driver_name: string }
  geofence: GeofenceRef
  location: { latitude: number; longitude: number }
}
