// Turkish dictionary — the source of truth for the key tree (see en.ts).
import * as core from './tr/core'
import * as ui from './tr/ui'
import * as pages from './tr/pages'
import * as setup from './tr/setup'
import { removal } from './tr/removal'

export const tr = { ...core, ...ui, ...pages, ...setup, removal }
