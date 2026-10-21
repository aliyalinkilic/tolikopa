// Alert sentences. The server sends `rule` + `params` next to a legacy, already
// worded `message`; when `params` is present the sentence is built here from a
// per-rule template so it follows the panel language. Every template has a bare
// form for missing params — nothing here may ever print "undefined".
import { translate } from '@/i18n'
import type { TKey } from '@/i18n'
import { formatDurationSec, formatPercent } from './format'
import type { AlertDTO } from './types'

type Params = Record<string, unknown>

function num(p: Params, ...keys: string[]): number | null {
  for (const k of keys) {
    const v = p[k]
    if (typeof v === 'number' && isFinite(v)) return v
    if (typeof v === 'string' && v.trim() !== '' && isFinite(Number(v))) return Number(v)
  }
  return null
}

function str(p: Params, ...keys: string[]): string | null {
  for (const k of keys) {
    const v = p[k]
    if (typeof v === 'string' && v.trim() !== '') return v.trim()
  }
  return null
}

/** A duration from whichever unit the server chose to send. */
function seconds(p: Params, base: string): number | null {
  const s = num(p, `${base}_seconds`, `${base}_secs`, base === 'age' ? 'seconds' : `${base}_s`)
  if (s !== null) return s
  const m = num(p, `${base}_minutes`, 'minutes')
  return m === null ? null : m * 60
}

type Builder = (host: string, p: Params, a: AlertDTO) => string

/** High-usage rules share one shape: "{host} …: {value}". */
function sustained(full: TKey, bare: TKey): Builder {
  return (host, p, a) => {
    const value = num(p, 'value', 'pct', 'percent', 'avg') ?? (typeof a.value === 'number' ? a.value : null)
    return value === null ? translate(bare, { host }) : translate(full, { host, value: formatPercent(value, 0) })
  }
}

const BUILDERS: Record<string, Builder> = {
  // params.kind (work-hours devices, docs/API.md §6.1): not_started (info),
  // went_offline (warning), stale (info, long-lived). Servers send no kind.
  offline: (host, p, a) => {
    const age = seconds(p, 'age') ?? (typeof a.value === 'number' ? a.value : null)
    const kind = str(p, 'kind')
    if (kind === 'not_started') return translate('alertText.offlineNotStarted', { host })
    if (kind === 'went_offline') {
      return age === null
        ? translate('alertText.offlineWentOfflineBare', { host })
        : translate('alertText.offlineWentOffline', { host, duration: formatDurationSec(age) })
    }
    if (kind === 'stale') {
      return age === null
        ? translate('alertText.offlineStaleBare', { host })
        : translate('alertText.offlineStale', { host, duration: formatDurationSec(age) })
    }
    return age === null
      ? translate('alertText.offlineBare', { host })
      : translate('alertText.offline', { host, duration: formatDurationSec(age) })
  },
  cpu_high: sustained('alertText.cpu_high', 'alertText.cpu_highBare'),
  mem_high: sustained('alertText.mem_high', 'alertText.mem_highBare'),
  ram_high: sustained('alertText.mem_high', 'alertText.mem_highBare'),
  net_high: sustained('alertText.net_high', 'alertText.net_highBare'),
  disk_low: (host, p, a) => {
    const free = num(p, 'free_pct', 'value') ?? (typeof a.value === 'number' ? a.value : null)
    const mount = str(p, 'mount', 'disk', 'volume')
    if (free === null) return translate('alertText.disk_lowBare', { host })
    const value = formatPercent(free, 1)
    return mount
      ? translate('alertText.disk_low', { host, mount, value })
      : translate('alertText.disk_lowNoMount', { host, value })
  },
  disk_full: (host, p, a) => BUILDERS.disk_low(host, p, a),
  smart_fail: (host, p) => {
    const disk = str(p, 'disk', 'device', 'model')
    return disk ? translate('alertText.smart_fail', { host, disk }) : translate('alertText.smart_failBare', { host })
  },
  crash_storm: (host, p, a) => {
    const raw = num(p, 'count', 'crashes') ?? (typeof a.value === 'number' ? a.value : null)
    if (raw === null) return translate('alertText.crash_stormBare', { host })
    const count = Math.round(raw) // also selects the plural form
    const win = seconds(p, 'window')
    if (win === null) return translate('alertText.crash_stormNoWindow', { host, count })
    // `app` (faulting process base name, most frequent in the window) is set
    // by servers from 0019 on; almost every storm is one crash-looping app.
    const app = str(p, 'app')
    return app
      ? translate('alertText.crash_stormApp', { host, app, count, duration: formatDurationSec(win) })
      : translate('alertText.crash_storm', { host, count, duration: formatDurationSec(win) })
  },
  app_crash: (host, p, a) => BUILDERS.crash_storm(host, p, a),
}

/**
 * The sentence to show for an alert: built from `params` when the server sent
 * them and we know the rule, otherwise the server's own `message`.
 */
export function alertMessage(a: AlertDTO): string {
  const params = a.params
  const build = BUILDERS[a.rule]
  if (params && typeof params === 'object' && build) {
    const host = str(params, 'hostname', 'host') ?? a.hostname ?? translate('alertText.thisMachine')
    return build(host, params, a)
  }
  return a.message ?? ''
}
