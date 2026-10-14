import { Fragment } from 'react'
import type { ReactNode } from 'react'
import { template, useLang } from './index'
import type { TKey } from './index'

type Tags = Record<string, (children: ReactNode) => ReactNode>
type Values = Record<string, ReactNode>

const TOKEN = /\{(\w+)\}|<(\w+)>([\s\S]*?)<\/\2>/g

function parse(src: string, values: Values, tags: Tags, prefix: string): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  let i = 0
  for (const m of src.matchAll(TOKEN)) {
    const at = m.index ?? 0
    if (at > last) out.push(src.slice(last, at))
    const key = `${prefix}${i++}`
    if (m[1] !== undefined) {
      const v = values[m[1]]
      out.push(<Fragment key={key}>{v === undefined || v === null ? '' : v}</Fragment>)
    } else {
      const inner = parse(m[3], values, tags, `${key}.`)
      const wrap = tags[m[2]]
      out.push(<Fragment key={key}>{wrap ? wrap(inner) : inner}</Fragment>)
    }
    last = at + m[0].length
  }
  if (last < src.length) out.push(src.slice(last))
  return out
}

interface TransProps {
  k: TKey
  /** `{name}` placeholders; values may be elements. `count` also picks the plural. */
  values?: Values
  /** `<b>…</b>`-style markers in the dictionary string → wrappers. */
  tags?: Tags
}

/**
 * Translated sentence with inline markup. The dictionary string keeps the whole
 * sentence in one piece — word order differs between languages, so it must never
 * be assembled from fragments in JSX.
 */
export function Trans({ k, values = {}, tags = {} }: TransProps) {
  useLang()
  const count = typeof values.count === 'number' ? values.count : undefined
  return <>{parse(template(k, count), values, tags, '')}</>
}
