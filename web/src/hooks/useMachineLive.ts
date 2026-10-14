import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { wsClient } from '@/lib/ws'
import type { MachineDetail, WSEnvelope } from '@/lib/types'

export interface LiveMetric {
  cpu: number
  mem: number
  t: string
}

/** Live CPU/RAM for one machine; also patches the ['machine', id] detail cache. */
export function useMachineLive(id: string | undefined) {
  const qc = useQueryClient()
  const [live, setLive] = useState<LiveMetric | null>(null)
  const [pulse, setPulse] = useState(0)
  const seriesRef = useRef<LiveMetric[]>([])

  useEffect(() => {
    if (!id) return
    seriesRef.current = []
    setLive(null)

    const off = wsClient.on((msg: WSEnvelope) => {
      if (msg.type === 'machine.metric' && msg.payload.machine_id === id) {
        const { cpu_pct, mem_ratio, t } = msg.payload
        const point: LiveMetric = { cpu: cpu_pct, mem: mem_ratio, t }
        seriesRef.current = [...seriesRef.current.slice(-59), point]
        setLive(point)
        setPulse((p) => p + 1)
        qc.setQueryData<MachineDetail>(['machine', id], (prev) =>
          prev ? { ...prev, cpu_pct, mem_ratio } : prev,
        )
      } else if (msg.type === 'fleet.update') {
        const m = msg.payload.machines?.find((x) => x.machine_id === id)
        if (m) {
          qc.setQueryData<MachineDetail>(['machine', id], (prev) => (prev ? { ...prev, ...m } : prev))
          setPulse((p) => p + 1)
        }
      }
    })
    const unsub = wsClient.subscribe([`machine:${id}`, 'fleet'])
    return () => {
      off()
      unsub()
    }
  }, [id, qc])

  return { live, pulse, series: seriesRef.current }
}
