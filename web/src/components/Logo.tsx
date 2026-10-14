import { useId } from 'react'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'
import { usePublicSettings } from '@/hooks/usePublicSettings'

interface LogoMarkProps {
  size?: number
  className?: string
  animated?: boolean
}

/**
 * The Tolikopa mark: concentric arcs closing to a pupil — a sentinel's eye that
 * echoes the ring gauges used throughout the panel. This is the brand through-line.
 */
export function LogoMark({ size = 28, className, animated = false }: LogoMarkProps) {
  const t = useT()
  const id = useId()
  const grad = `gz-logo-${id}`
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      className={className}
      role="img"
      aria-label={t('common.appName')}
    >
      <defs>
        <linearGradient id={grad} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="var(--color-brand-1)" />
          <stop offset="100%" stopColor="var(--color-brand-2)" />
        </linearGradient>
      </defs>
      {/* outer sentinel arc — an open gauge */}
      <circle
        cx="16"
        cy="16"
        r="13"
        stroke={`url(#${grad})`}
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeDasharray="62 20"
        className={animated ? 'logo-arc logo-arc-spin' : 'logo-arc'}
      />
      {/* inner dotted ring */}
      <circle
        cx="16"
        cy="16"
        r="8"
        stroke={`url(#${grad})`}
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeDasharray="1.5 5"
        opacity="0.55"
      />
      {/* pupil */}
      <circle cx="16" cy="16" r="3.4" fill={`url(#${grad})`} />
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

interface LogoProps {
  className?: string
  markSize?: number
  showWordmark?: boolean
  subtitle?: boolean
}

export function Logo({ className, markSize = 30, showWordmark = true, subtitle = false }: LogoProps) {
  const t = useT()
  const brand = useBrandSubtitle()
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-2.5', className)}>
      <LogoMark size={markSize} />
      {showWordmark && (
        <span className="flex min-w-0 flex-col leading-none">
          <span className="text-[18px] font-bold tracking-[-0.02em] text-text">{t('common.appName')}</span>
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
