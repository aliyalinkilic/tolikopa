// Whether THIS browser is in the middle of the first-run wizard, and where.
//
// The server only knows "an admin exists" and "setup completed". An installation
// that was upgraded from before the wizard — or an admin who chose "finish later" —
// must not be forced back into it (they get a banner instead), so being "in the
// wizard" is a browser-side marker: set when the wizard creates the first admin or
// when an admin reopens it, cleared on Finish / Finish Later. It survives a reload;
// the step it names is only a hint and is clamped by what the server says is done.
import { useSyncExternalStore } from 'react'

export const WIZARD_STEPS = ['welcome', 'account', 'server', 'domain', 'download', 'verify'] as const
export type WizardStep = (typeof WIZARD_STEPS)[number]

const KEY = 'tolikopa-wizard'
const listeners = new Set<() => void>()

function read(): WizardStep | null {
  try {
    const v = localStorage.getItem(KEY)
    return (WIZARD_STEPS as readonly string[]).includes(v ?? '') ? (v as WizardStep) : null
  } catch {
    return null
  }
}

let current: WizardStep | null = read()

export function getWizardStep(): WizardStep | null {
  return current
}

export function setWizardStep(step: WizardStep | null): void {
  current = step
  try {
    if (step) localStorage.setItem(KEY, step)
    else localStorage.removeItem(KEY)
  } catch {
    // storage blocked: the wizard still works for this page load
  }
  listeners.forEach((l) => l())
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

const OFFERED_KEY = 'tolikopa-wizard-offered'

/**
 * Opens the wizard once per browser for an admin of an installation whose setup
 * was never finished — the case where the first admin was created on the server
 * by setup.sh rather than in the wizard. "Finish later" is respected afterwards:
 * the banner takes over.
 */
export function offerWizardOnce(): void {
  try {
    if (localStorage.getItem(OFFERED_KEY)) return
    localStorage.setItem(OFFERED_KEY, '1')
  } catch {
    return // storage blocked: never force the wizard on every page load
  }
  if (current === null) setWizardStep('server')
}

/** The stored step, or null when this browser is not in the wizard. */
export function useWizardStep(): WizardStep | null {
  return useSyncExternalStore(subscribe, getWizardStep, getWizardStep)
}
