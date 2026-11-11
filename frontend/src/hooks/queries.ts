import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import type { Category } from '../lib/types'

export const keys = {
  geofences: ['geofences'] as const,
  vehicles: ['vehicles'] as const,
  vehicleLocation: (id: string) => ['vehicle-location', id] as const,
  rules: ['alert-rules'] as const,
  violations: ['violations'] as const,
}

export function useGeofences(category: Category | '' = '') {
  return useQuery({
    queryKey: [...keys.geofences, category],
    queryFn: () => api.listGeofences(category),
    select: (d) => d.geofences,
  })
}

/** Polls so markers follow vehicles moved by other clients or the simulator. */
export function useVehicles() {
  return useQuery({
    queryKey: keys.vehicles,
    queryFn: api.listVehicles,
    select: (d) => d.vehicles,
    refetchInterval: 5000,
  })
}

export function useVehicleLocation(id: string | null) {
  return useQuery({
    queryKey: keys.vehicleLocation(id ?? ''),
    queryFn: () => api.vehicleLocation(id!),
    enabled: !!id,
  })
}

export function useAlertRules(filters: { geofence_id?: string; vehicle_id?: string }) {
  return useQuery({
    queryKey: [...keys.rules, filters],
    queryFn: () => api.listAlertRules(filters),
    select: (d) => d.alerts,
  })
}

export function useViolations(filters: Parameters<typeof api.violations>[0]) {
  return useQuery({
    queryKey: [...keys.violations, filters],
    queryFn: () => api.violations(filters),
    placeholderData: (prev) => prev,
  })
}

export function useCreateGeofence() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createGeofence,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.geofences }),
  })
}

export function useCreateVehicle() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createVehicle,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.vehicles }),
  })
}

export function useUpdateLocation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.updateLocation,
    onSuccess: (_d, vars) => {
      qc.invalidateQueries({ queryKey: keys.vehicles })
      qc.invalidateQueries({ queryKey: keys.vehicleLocation(vars.vehicle_id) })
      qc.invalidateQueries({ queryKey: keys.violations })
    },
  })
}

export function useConfigureAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.configureAlert,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.rules }),
  })
}

export function useDeactivateAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.deactivateAlertRule,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.rules }),
  })
}
