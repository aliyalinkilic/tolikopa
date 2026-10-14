// Shared motion primitives (durations/easings from docs/DESIGN-SYSTEM.md).
import type { Transition, Variants } from 'motion/react'

const EASE_OUT: [number, number, number, number] = [0.22, 1, 0.36, 1]

export const DUR = { fast: 0.12, base: 0.2, slow: 0.38 } as const

export const easeOut: Transition = { duration: DUR.base, ease: EASE_OUT }
export const easeOutSlow: Transition = { duration: DUR.slow, ease: EASE_OUT }
export const springy: Transition = { type: 'spring', stiffness: 220, damping: 26 }

/** opacity 0→1 + y 8px→0 entrance. */
export const fadeUp: Variants = {
  hidden: { opacity: 0, y: 8 },
  show: { opacity: 1, y: 0, transition: easeOut },
}

export const fade: Variants = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: easeOut },
}

/** Container that staggers its children on mount. */
export function staggerContainer(stagger = 0.03, delayChildren = 0.02): Variants {
  return {
    hidden: {},
    show: { transition: { staggerChildren: stagger, delayChildren } },
  }
}

/** Route transition: gentle fade + slide. */
export const routeVariants: Variants = {
  hidden: { opacity: 0, y: 10 },
  show: { opacity: 1, y: 0, transition: easeOut },
  exit: { opacity: 0, y: -8, transition: { duration: DUR.fast, ease: EASE_OUT } },
}
