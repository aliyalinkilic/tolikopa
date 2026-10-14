import { useId } from 'react'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'
import { usePublicSettings } from '@/hooks/usePublicSettings'

interface LogoMarkProps {
  size?: number
  className?: string
  animated?: boolean
}

/* Mark geometry, on a 32-unit grid. A flat-top hexagon (one cell of the fleet)
   with a health pulse cut clean through it, vertex to vertex. The pulse is a mask
   knock-out rather than a painted line, so it always shows whatever surface the
   mark sits on and reads correctly in both themes. Keep in sync with
   public/favicon.svg and docs/logo*.svg. */
const HEX = 'M29 16 22.5 27.26H9.5L3 16 9.5 4.74H22.5Z'
const PULSE = 'M2 16H9.5L12 19.5 15.5 8.5 19 23.5 21.5 16H30'

/**
 * The Tolikopa mark: a hexagonal fleet cell with a heartbeat running through it —
 * "keeping a fleet's health in view", with no eye or lens imagery. With `animated`
 * the pulse is traced in and out like a monitor sweep (loading states only); the
 * resting mark is static and reduced-motion users always get the static mark.
 */
export function LogoMark({ size = 28, className, animated = false }: LogoMarkProps) {
  const t = useT()
  const id = useId()
  const grad = `tk-logo-g-${id}`
  const mask = `tk-logo-m-${id}`
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      className={cn('shrink-0', className)}
      role="img"
      aria-label={t('common.appName')}
    >
      <defs>
        <linearGradient id={grad} x1="3" y1="4" x2="29" y2="28" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="var(--color-brand-1)" />
          <stop offset="100%" stopColor="var(--color-brand-2)" />
        </linearGradient>
        <mask id={mask} maskUnits="userSpaceOnUse" x="0" y="0" width="32" height="32">
          {/* the cell, corners softened by a round-joined stroke */}
          <path d={HEX} fill="#fff" stroke="#fff" strokeWidth="2" strokeLinejoin="round" />
          {/* the pulse, knocked out */}
          <path
            d={PULSE}
            pathLength={1}
            stroke="#000"
            strokeWidth="2.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            className={animated ? 'logo-pulse logo-pulse-trace' : 'logo-pulse'}
          />
        </mask>
      </defs>
      <rect width="32" height="32" fill={`url(#${grad})`} mask={`url(#${mask})`} />
    </svg>
  )
}

/** The line under the wordmark: the organisation's name when the server has one,
 *  the generic product tagline otherwise. */
export function useBrandSubtitle(): string {
  const t = useT()
  const { data } = usePublicSettings()
  return data?.org_name?.trim() || t('common.tagline')
}

/** Renders the product name with its trailing capitalised run ("KOPA" in
 *  "ToliKOPA") carried in the accent gradient. Any name without such a run is
 *  rendered plain, so a changed dictionary entry never breaks. */
export function Wordmark({ className }: { className?: string }) {
  const t = useT()
  const name = t('common.appName')
  const m = /^(.*?\p{Ll})(\p{Lu}{2,})$/u.exec(name)
  return (
    <span className={cn('font-bold tracking-[-0.02em] text-text', className)}>
      {m ? (
        <>
          {m[1]}
          <span className="text-gradient tracking-[0.005em]">{m[2]}</span>
        </>
      ) : (
        name
      )}
    </span>
  )
}

interface LogoProps {
  className?: string
  markSize?: number
  showWordmark?: boolean
  subtitle?: boolean
}

export function Logo({ className, markSize = 30, showWordmark = true, subtitle = false }: LogoProps) {
  const brand = useBrandSubtitle()
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-2.5', className)}>
      <LogoMark size={markSize} />
      {showWordmark && (
        <span className="flex min-w-0 flex-col leading-none">
          <Wordmark className="text-[18px]" />
          {subtitle && (
            <span
              className="mt-0.5 truncate text-[10px] font-medium uppercase tracking-[0.22em] text-text-dim"
              title={brand}
            >
              {brand}
            </span>
          )}
        </span>
      )}
    </span>
  )
}
