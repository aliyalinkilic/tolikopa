// English — shared components.
import { plural } from '../plural'
import type { Shape } from '../plural'
import type * as tr from '../tr/ui'

export const range: Shape<typeof tr.range> = {
  '1h': { label: '1 hour', short: '1h' },
  '6h': { label: '6 hours', short: '6h' },
  '24h': { label: '24 hours', short: '24h' },
  '7d': { label: '7 days', short: '7d' },
  '30d': { label: '30 days', short: '30d' },
  custom: { label: 'Custom range', short: 'Custom' },
}

export const ui: Shape<typeof tr.ui> = {
  table: {
    empty: 'No records found',
    prev: 'Previous',
    next: 'Next',
    prevPage: 'Previous page',
    nextPage: 'Next page',
  },
  range: {
    group: 'Time range',
    customDialog: 'Custom time range',
    from: 'From',
    to: 'To',
    invalid: 'The start must be before the end.',
  },
  heatmap: {
    caption: 'Heatmap of active minutes by weekday and hour',
    hour: 'Hour {hour}',
    none: 'None',
    low: 'Less',
    high: 'More',
  },
  timeline: {
    empty: 'No sessions in this range',
    sessions: 'Sessions',
    active: 'Active',
    loggedOn: 'Logged On',
    ongoing: 'ongoing',
  },
  chart: {
    empty: 'No data in this range',
    samples: plural('{count} sample', '{count} samples'),
    noCategory: 'No category data',
  },
  confirm: {
    typeNumber: 'To confirm, type the number below',
  },
  machineCard: {
    updates: plural('{count} update', '{count} updates'),
    reboot: 'Restart Pending',
    openDetail: 'Open machine details for {host}',
  },
  alertList: {
    none: 'No open alerts',
    allGood: 'Everything looks fine.',
    ack: 'Acknowledge',
  },
  password: {
    ruleLength: 'At least {count} characters',
    ruleMatch: 'Both passwords match',
    ruleDifferent: 'Different from the current password',
  },
  copy: {
    failed: 'Could not copy — select the text and copy it by hand.',
  },
}
