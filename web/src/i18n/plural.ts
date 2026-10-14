// Dictionary leaf types. A leaf is either a plain string or a plural pair picked
// with Intl.PluralRules on the `count` param. The brand keeps a plural from being
// mistaken for a namespace that happens to contain an `other` key.

export interface PluralForms {
  readonly __plural: true
  readonly one: string
  readonly other: string
}

export type Leaf = string | PluralForms

/** `plural('{count} machine', '{count} machines')`. Turkish nouns do not inflect
 *  after a numeral, so tr dictionaries normally use a plain string instead. */
export function plural(one: string, other: string): PluralForms {
  return { __plural: true, one, other }
}

export function isPlural(v: unknown): v is PluralForms {
  return typeof v === 'object' && v !== null && (v as { __plural?: unknown }).__plural === true
}

/** Same key tree as T, but every leaf may be a string or a plural in either
 *  language. `en` is typed as `Shape<typeof tr>`, so a missing or misspelled key
 *  is a compile error. */
export type Shape<T> = { [K in keyof T]: T[K] extends Leaf ? Leaf : Shape<T[K]> }

/** Dotted paths to every leaf of a dictionary. */
export type Paths<T> = {
  [K in keyof T & string]: T[K] extends Leaf ? K : `${K}.${Paths<T[K]>}`
}[keyof T & string]
