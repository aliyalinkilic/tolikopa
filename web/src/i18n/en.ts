// English dictionary. Typed against the Turkish one: a missing, extra or misspelled
// key is a compile error.
import * as core from './en/core'
import * as ui from './en/ui'
import * as pages from './en/pages'
import * as setup from './en/setup'
import { removal } from './en/removal'
import type { Shape } from './plural'
import type { tr } from './tr'

export const en: Shape<typeof tr> = { ...core, ...ui, ...pages, ...setup, removal }
