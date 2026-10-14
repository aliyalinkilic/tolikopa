// Client-side fallback for the "slow machines" report.
//
// The real ranking is computed server-side (GET /api/stats/slow-machines) from
// multi-day history: sustained memory pressure, total RAM, core count, SMART,
// crash counts and uptime. When that endpoint is not deployed yet, this derives a
// deliberately weaker ranking from the live fleet snapshot the panel already has.
//
// It is NOT a reimplementation — /api/fleet carries no mem_total, no cpu_cores and
// no boot_time, so only two factors survive and both are instantaneous rather than
// sustained. The page labels this clearly instead of passing it off as the report.
import { compareText } from './format'
import type { FleetMachine, SlowMachine, SlowReason } from './types'

const W_MEM = 30
const W_DISK = 20
const W_ALERTS = 15

const SCORE_WARN = 25
const SCORE_CRIT = 50

function scaled(v: number, lo: number, hi: number, weight: number): number {
  if (v <= lo || hi <= lo) return 0
  if (v >= hi) return weight
  return Math.round((weight * (v - lo)) / (hi - lo))
}

function severityOf(score: number): SlowMachine['severity'] {
  if (score >= SCORE_CRIT) return 'crit'
  if (score >= SCORE_WARN) return 'warn'
  return 'ok'
}

/** Rank online machines by a snapshot-only slowness estimate, worst first. */
export function deriveSlowMachines(fleet: FleetMachine[], limit = 20): SlowMachine[] {
  const out: SlowMachine[] = []

  for (const m of fleet) {
    if (!m.online) continue // an offline machine's last snapshot says nothing about speed
    if (m.kind === 'linux') continue // the report ranks user PCs; the server API excludes Linux too

    const reasons: SlowReason[] = []
    let total = 0
    const add = (key: SlowReason['key'], points: number, value: number) => {
      if (points <= 0) return
      reasons.push({ key, points, value })
      total += points
    }

    add('mem_pressure', scaled(m.mem_ratio, 0.7, 0.95, W_MEM), m.mem_ratio)

    // disk_min_free_pct is FREE percent on the worst mount; the report speaks in used.
    const usedPct = 100 - (m.disk_min_free_pct ?? 100)
    add('disk_full', scaled(usedPct, 85, 98, W_DISK), usedPct)

    // Open alerts stand in for the crash/SMART factors we cannot see from here.
    add('crashes', Math.min(W_ALERTS, (m.open_alerts ?? 0) * 5), m.open_alerts ?? 0)

    reasons.sort((a, b) => b.points - a.points)
    const score = Math.min(100, total)

    out.push({
      machine_id: m.machine_id,
      hostname: m.hostname,
      logon_user: m.logon_user,
      online: m.online,
      last_seen: m.last_seen,
      score,
      severity: severityOf(score),
      reasons,
      mem_total: 0,
      mem_ratio_avg: m.mem_ratio,
      mem_ratio_max: m.mem_ratio,
      cpu_avg: m.cpu_pct,
      disk_used_pct: usedPct,
      disk_free_bytes: 0,
      uptime_days: 0,
      crashes: 0,
      smart_bad: false,
      sample_hours: 0, // 0 marks "snapshot only" — the page keys its notice off this
    })
  }

  out.sort((a, b) => (b.score - a.score) || compareText(a.hostname, b.hostname))
  return out.filter((m) => m.score > 0).slice(0, limit)
}
