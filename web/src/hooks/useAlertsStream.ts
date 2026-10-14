import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { wsClient } from '@/lib/ws'
import { useToast } from './useToast'
import { ruleLabel, severityToken } from '@/lib/labels'
import { alertMessage } from '@/lib/alertText'
import type { WSEnvelope } from '@/lib/types'

/** Global alert stream → toasts + cache invalidation. Mount once inside the shell. */
export function useAlertsStream() {
  const qc = useQueryClient()
  const { push } = useToast()

  useEffect(() => {
    const invalidateAll = () => {
      qc.invalidateQueries({ queryKey: ['alerts'] })
      qc.invalidateQueries({ queryKey: ['fleet'] })
      qc.invalidateQueries({ queryKey: ['fleet-summary'] })
    }

    const off = wsClient.on((msg: WSEnvelope) => {
      if (msg.type === 'alert.opened') {
        const a = msg.payload
        push({
          title: `${ruleLabel(a.rule)} — ${a.hostname ?? a.machine_id}`,
          description: alertMessage(a),
          tone: severityToken(a.severity),
          href: `/machines/${a.machine_id}`,
        })
        invalidateAll()
      } else if (msg.type === 'alert.resolved') {
        invalidateAll()
      }
    })
    const unsub = wsClient.subscribe(['alerts'])
    return () => {
      off()
      unsub()
    }
  }, [qc, push])
}
