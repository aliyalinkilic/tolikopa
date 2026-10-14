import { useEffect } from 'react'
import { animate, motion, useMotionValue, useReducedMotion, useTransform } from 'motion/react'

interface CountUpProps {
  value: number
  format?: (n: number) => string
  duration?: number
  className?: string
}

/** Animated count-up for KPI numbers (motion useMotionValue → useTransform). */
export function CountUp({ value, format = (n) => String(Math.round(n)), duration = 0.38, className }: CountUpProps) {
  const reduce = useReducedMotion()
  const mv = useMotionValue(reduce ? value : 0)
  const text = useTransform(mv, (v) => format(v))

  useEffect(() => {
    if (reduce) {
      mv.set(value)
      return
    }
    const controls = animate(mv, value, { duration, ease: [0.22, 1, 0.36, 1] })
    return () => controls.stop()
  }, [value, duration, reduce, mv])

  return <motion.span className={className}>{text}</motion.span>
}
