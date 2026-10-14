import { useMemo } from 'react'
import { encode } from 'uqr'

interface QrCodeProps {
  value: string
  /** Rendered edge length in CSS pixels. */
  size?: number
  label: string
}

/**
 * QR code as inline SVG (uqr builds the module matrix; nothing leaves the page).
 * Always dark-on-white with a quiet zone, in both themes — authenticator apps
 * need the contrast and the white margin to lock on.
 */
export function QrCode({ value, size = 184, label }: QrCodeProps) {
  const { path, cells } = useMemo(() => {
    const qr = encode(value, { ecc: 'M', border: 3 })
    let d = ''
    qr.data.forEach((row, y) => {
      row.forEach((on, x) => {
        if (on) d += `M${x} ${y}h1v1h-1z`
      })
    })
    return { path: d, cells: qr.size }
  }, [value])

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${cells} ${cells}`}
      role="img"
      aria-label={label}
      shapeRendering="crispEdges"
      className="shrink-0 rounded-[var(--radius-md)] border border-border-2"
    >
      <rect width={cells} height={cells} fill="#ffffff" />
      <path d={path} fill="#000000" />
    </svg>
  )
}
