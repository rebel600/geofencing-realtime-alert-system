// leaflet-geoman is a classic Leaflet plugin that patches the global `L`.
// ES imports are hoisted, so this must live in its own module imported first.
import L from 'leaflet'

;(window as unknown as { L: typeof L }).L = L
