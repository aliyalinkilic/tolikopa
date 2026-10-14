// English text for the agent's compiled removal catalog.
//
// The catalog (agent/internal/appcatalog/catalog.json) ships its display names and
// notes in Turkish, and changing it would change its hash on every agent. So the
// panel localises it on the client: keyed by catalog `item_id`, English only — in
// Turkish the server's own text is shown. An item missing here (a newer agent
// catalog) falls back to the server text.
import { getLang } from './index'

interface CatalogText {
  /** Only where the catalog name is a description rather than a product name. */
  name?: string
  notes?: string
}

const STARTUP = ' — at sign-in'

const EN: Record<string, CatalogText> = {
  weather: { name: 'Weather (MSN)', notes: 'WebView2-based web wrapper; 500–600 MB of RAM idle, 1.6 GB when the map is in use.' },
  news: { name: 'News (MSN)', notes: 'Same WebView2 plumbing and the same ad load as Weather.' },
  solitaire: { name: 'Solitaire Collection', notes: 'Ad-supported game. On Pro editions ContentDeliveryManager may reinstall it.' },
  xbox_app_legacy: { name: 'Xbox Console Companion (legacy)', notes: 'Found on Windows 10 images.' },
  xbox_game_overlay: { name: 'Xbox Game Bar plug-in' },
  xbox_tcui: { name: 'Xbox live experience component', notes: 'Some Store games depend on it for sign-in.' },
  xbox_speech_overlay: { name: 'Xbox speech-to-text overlay' },
  mixed_reality: { name: 'Mixed Reality Portal', notes: 'Deprecated by Microsoft.' },
  feedback_hub: { name: 'Feedback Hub' },
  get_help: { name: 'Get Help' },
  get_started: { name: 'Tips' },
  office_hub: { name: 'Office (promotional shortcut)', notes: 'Does not affect an installed Office; it is only the Store promo shell.' },
  teams_consumer: {
    name: 'Teams (personal / Chat)',
    notes: 'The consumer edition. Work Teams is a separate package (MSTeams_8wekyb3d8bbwe) and is not in this list.',
  },
  people: { name: 'People' },
  maps: { name: 'Maps' },
  clipchamp: { name: 'Clipchamp video editor', notes: 'The publisher hash can differ from image to image — verify on a reference machine.' },
  zune_video: { name: 'Movies & TV' },
  zune_music: { notes: 'The default media player on Windows 11; if removed, audio/video files will not open.' },
  mail_calendar: { name: 'Mail and Calendar', notes: 'Deprecated by Microsoft, but some staff may still use it — ask first.' },
  your_phone: { name: 'Phone Link' },
  sticky_notes: { name: 'Sticky Notes', notes: 'If removed, the user loses access to their saved notes.' },
  sound_recorder: { name: 'Sound Recorder' },
  alarms_clock: { name: 'Clock (Alarms & Clock)', notes: 'The focus sessions feature lives here; some people may use it.' },
  quick_assist: { name: 'Quick Assist', notes: 'The IT team may be using it for remote support — take care.' },
  dev_home: { notes: 'Has no use in a fleet without developers.' },
  outlook_new: { name: 'New Outlook', notes: 'Windows Update may reinstall it; it does not affect classic Outlook.' },
  copilot: { notes: 'On 24H2 and later the AI components are protected; removal may be refused with event 874.' },
  bing_search: {
    name: 'Start menu web search',
    notes: 'DANGEROUS: can break Start menu search on 25H2. Try it on a single machine first.',
  },
  'startup.teams': { name: `Teams (personal)${STARTUP}` },
  'startup.spotify': { name: `Spotify${STARTUP}` },
  'startup.steam': { name: `Steam${STARTUP}` },
  'startup.epic': { name: `Epic Games${STARTUP}` },
  'startup.discord': { name: `Discord${STARTUP}` },
  'startup.adobe_updater': {
    name: `Adobe updater${STARTUP}`,
    notes: 'The real value name carries a version suffix (AdobeAAMUpdater-1.0).',
  },
  'startup.adobe_gc': {
    name: `Adobe licence checker${STARTUP}`,
    notes: 'The real value name carries a version suffix (AdobeGCInvoker-1.0).',
  },
  'startup.skype': { name: `Skype${STARTUP}` },
  'startup.itunes': { name: `iTunes helper${STARTUP}` },
  'startup.java': { name: `Java updater${STARTUP}` },
  'startup.ccleaner': { name: `CCleaner${STARTUP}` },
  'startup.zoom': { name: `Zoom${STARTUP}`, notes: 'Can slow down joining a meeting; some departments may rely on it.' },
  'startup.onedrive': {
    name: `OneDrive${STARTUP}`,
    notes: 'CAUTION: if the organisation syncs files with OneDrive, turning this off stops the sync.',
  },
  'startup.edge': {
    name: 'Edge auto-launch',
    notes: 'The value name carries a machine-specific suffix; this is a single prefix-matched catalog entry.',
  },
  'startup.onenote': { name: `Send to OneNote${STARTUP}` },
}

/** Display name for a catalog item in the active language. */
export function catalogName(itemId: string, serverName: string | null | undefined): string {
  const local = getLang() === 'en' ? EN[itemId]?.name : undefined
  return local ?? serverName ?? itemId
}

/** Notes for a catalog item in the active language (may be empty). */
export function catalogNotes(itemId: string, serverNotes: string | null | undefined): string {
  if (!serverNotes) return ''
  const local = getLang() === 'en' ? EN[itemId]?.notes : undefined
  return local ?? serverNotes
}
