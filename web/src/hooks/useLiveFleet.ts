import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { wsClient } from '@/lib/ws'
import type { FleetMachine, WSEnvelope } from '@/lib/types'

/**
 * Subscribes to the "fleet" channel and merges pushes into every ['fleet', …] list
 * query. Returns a per-machine `pulses` counter that bumps on each update so cards
 * can flash. The ['fleet-summary'] query is intentionally NOT matched here.
 */
export function useLiveFleet() {
  const qc = useQueryClient()
  const [pulses, setPulses] = useState<Record<string, number>>({})
  const pulseRef = useRef<Record<string, number>>({})

  useEffect(() => {
    const bump = (ids: string[]) => {
      if (ids.length === 0) return
      const next = { ...pulseRef.current }
      for (const id of ids) next[id] = (next[id] ?? 0) + 1
      pulseRef.current = next
      setPulses(next)
    }

    const merge = (incoming: FleetMachine[]) => {
      const incomingById = new Map(incoming.map((m) => [m.machine_id, m]))
      // Patch machines ALREADY present in each ['fleet', …] query; never add new
      // ones. A filtered query (e.g. ['fleet', {tag:'Personel'}]) holds only its
      // server-filtered set, but fleet.update pushes carry *all* changed machines
      // regardless of filter — adding them here would leak non-matching machines
      // into the filtered grid one by one. Genuinely new machines surface on the
      // next REST refetch, which re-applies the server-side filter.
      qc.setQueriesData<FleetMachine[]>({ queryKey: ['fleet'] }, (prev) => {
        if (!prev) return prev
        let changed = false
        const next = prev.map((m) => {
          const inc = incomingById.get(m.machine_id)
          if (!inc) return m
          changed = true
          return { ...m, ...inc }
        })
        return changed ? next : prev
      })
    }

    const patchMetric = (id: string, cpu: number, mem: number) => {
      qc.setQueriesData<FleetMachine[]>({ queryKey: ['fleet'] }, (prev) =>
        prev?.map((m) => (m.machine_id === id ? { ...m, cpu_pct: cpu, mem_ratio: mem } : m)),
      )
    }

    const off = wsClient.on((msg: WSEnvelope) => {
      if (msg.type === 'fleet.update') {
        const machines = msg.payload.machines ?? []
        merge(machines)
        bump(machines.map((m) => m.machine_id))
      } else if (msg.type === 'machine.metric') {
        const p = msg.payload
        patchMetric(p.machine_id, p.cpu_pct, p.mem_ratio)
        bump([p.machine_id])
      }
    })
    const unsub = wsClient.subscribe(['fleet'])
    return () => {
      off()
      unsub()
    }
  }, [qc])

  return { pulses }
}
