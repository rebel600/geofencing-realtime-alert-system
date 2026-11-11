import { MapPin, PenLine, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'
import { useCreateGeofence, useGeofences } from '../../hooks/queries'
import { ApiError } from '../../lib/api'
import { CATEGORIES, CATEGORY_META, type Category, type LatLngPair } from '../../lib/types'
import { Button, Card, CategoryBadge, Empty, ErrorNote, Field, Input, LoadingRows, Section, Select, Textarea } from '../ui'
import type { MapMode } from '../MapView'

interface Props {
  mode: MapMode
  setMode: (m: MapMode) => void
  draft: LatLngPair[] | null
  setDraft: (d: LatLngPair[] | null) => void
  onFocus: (id: string) => void
}

const EXAMPLE = JSON.stringify(
  [
    [37.7749, -122.4194],
    [37.7849, -122.4194],
    [37.7849, -122.4094],
    [37.7749, -122.4094],
    [37.7749, -122.4194],
  ],
  null,
  0,
)

function parseCoordinates(text: string): LatLngPair[] {
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    throw new Error('Coordinates must be valid JSON, e.g. [[37.77, -122.41], ...]')
  }
  if (!Array.isArray(value) || !value.every((p) => Array.isArray(p) && p.length === 2 && p.every((n) => typeof n === 'number'))) {
    throw new Error('Coordinates must be an array of [latitude, longitude] number pairs')
  }
  return value as LatLngPair[]
}

export default function GeofencePanel({ mode, setMode, draft, setDraft, onFocus }: Props) {
  const [filter, setFilter] = useState<Category | ''>('')
  const list = useGeofences(filter)
  const create = useCreateGeofence()

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState<Category>('delivery_zone')
  const [coordsText, setCoordsText] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})

  // Keep the textarea in sync with polygons drawn on the map.
  const [lastDraft, setLastDraft] = useState(draft)
  if (draft !== lastDraft) {
    setLastDraft(draft)
    if (draft) setCoordsText(JSON.stringify(draft))
  }

  const drawing = mode === 'draw'

  const onCoordsChange = (text: string) => {
    setCoordsText(text)
    try {
      const parsed = parseCoordinates(text)
      setDraft(parsed.length >= 3 ? parsed : null)
    } catch {
      /* preview only updates on valid input */
    }
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const next: Record<string, string> = {}
    if (!name.trim()) next.name = 'Name is required'
    let coordinates: LatLngPair[] = []
    try {
      coordinates = parseCoordinates(coordsText)
    } catch (err) {
      next.coordinates = (err as Error).message
    }
    setErrors(next)
    if (Object.keys(next).length) return

    try {
      const res = await create.mutateAsync({ name: name.trim(), description: description.trim(), category, coordinates })
      toast.success(`Geofence "${res.name}" created`)
      setName('')
      setDescription('')
      setCoordsText('')
      setDraft(null)
      onFocus(res.id)
    } catch (err) {
      if (err instanceof ApiError && err.field) setErrors({ [err.field]: err.message })
      else toast.error(err instanceof Error ? err.message : 'Could not create geofence')
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Section title="New geofence">
        <form onSubmit={submit} className="flex flex-col gap-3" noValidate>
          <Field label="Name" htmlFor="gf-name" error={errors.name}>
            <Input id="gf-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Downtown Delivery Zone" />
          </Field>
          <Field label="Description (optional)" htmlFor="gf-desc">
            <Input id="gf-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Main delivery area" />
          </Field>
          <Field label="Category" htmlFor="gf-cat" error={errors.category}>
            <Select id="gf-cat" value={category} onChange={(e) => setCategory(e.target.value as Category)}>
              {CATEGORIES.map((c) => (
                <option key={c} value={c}>
                  {CATEGORY_META[c].label}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="Boundary"
            htmlFor="gf-coords"
            error={errors.coordinates}
            hint="Draw on the map, or paste [latitude, longitude] pairs. First and last point must match."
          >
            <div className="flex gap-2">
              <Button
                type="button"
                variant={drawing ? 'primary' : 'secondary'}
                size="sm"
                onClick={() => setMode(drawing ? 'idle' : 'draw')}
              >
                {drawing ? <X className="size-3.5" /> : <PenLine className="size-3.5" />}
                {drawing ? 'Cancel drawing' : 'Draw on map'}
              </Button>
              {draft && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setDraft(null)
                    setCoordsText('')
                  }}
                >
                  Clear
                </Button>
              )}
            </div>
            {drawing && (
              <p className="rounded-md bg-sky-50 px-2.5 py-2 text-xs text-sky-900 dark:bg-sky-500/10 dark:text-sky-200">
                Click on the map to add corners. Click the first corner to finish.
              </p>
            )}
            <Textarea
              id="gf-coords"
              rows={4}
              value={coordsText}
              onChange={(e) => onCoordsChange(e.target.value)}
              placeholder={EXAMPLE}
            />
          </Field>
          <Button type="submit" loading={create.isPending}>
            Create geofence
          </Button>
        </form>
      </Section>

      <Section
        title="Geofences"
        action={
          <Select
            aria-label="Filter by category"
            value={filter}
            onChange={(e) => setFilter(e.target.value as Category | '')}
            className="h-8 w-40 text-xs"
          >
            <option value="">All categories</option>
            {CATEGORIES.map((c) => (
              <option key={c} value={c}>
                {CATEGORY_META[c].label}
              </option>
            ))}
          </Select>
        }
      >
        {list.isPending ? (
          <LoadingRows />
        ) : list.isError ? (
          <ErrorNote error={list.error} onRetry={() => list.refetch()} />
        ) : list.data.length === 0 ? (
          <Empty>{filter ? 'No geofences in this category.' : 'No geofences yet. Draw one on the map to get started.'}</Empty>
        ) : (
          <ul className="flex flex-col gap-2">
            {list.data.map((g) => (
              <li key={g.id}>
                <Card className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">{g.name}</span>
                      <CategoryBadge category={g.category} />
                    </div>
                    {g.description && <p className="mt-0.5 text-xs text-slate-600 dark:text-slate-400">{g.description}</p>}
                    <p className="mt-1 font-mono text-[11px] text-slate-400">
                      {g.id} · {g.coordinates.length - 1} corners
                    </p>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => onFocus(g.id)} aria-label={`Show ${g.name} on map`}>
                    <MapPin className="size-3.5" />
                  </Button>
                </Card>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  )
}
