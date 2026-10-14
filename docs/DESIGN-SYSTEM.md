# Design System — Tolikopa Panel

A modern, professional IT operations panel. Calm but vivid; makes dense data readable, with
restrained animation. There are **two themes** (dark / light / system) and **two languages**
(Türkçe / English); both switch at runtime, without a page reload. Implementation: Tailwind v4
(`@theme` tokens) + `motion` + Recharts. Load the `frontend-design` and `dataviz` skills when doing
frontend development.

## Theme

The single source of truth is `web/src/index.css`. The `@theme static { … }` block carries the
values of the **dark** theme; `:root[data-theme="light"] { … }` redefines the same tokens for the
**light** theme. Components never hard-code a colour — they always use tokens (`bg-surface`,
`text-text-muted`, `var(--color-ok)` …).

- `<html data-theme="dark|light">` is set **before** first paint by a small inline script in
  `index.html` (prevents a flash of the wrong theme); after that `src/lib/prefs.ts` manages it.
  Both must use the same `localStorage` keys (`tolikopa-theme`, `tolikopa-lang`) and the same
  defaults. If a CSP containing `script-src 'self'` is added later, give this script a hash/nonce
  or move it into a same-origin, synchronously loaded file.
- Preference: `dark` · `light` · `system` (default `system`; tracks `prefers-color-scheme` live).
  `color-scheme` and `<meta name="theme-color">` are also updated together with the theme.
- Resolution order (same for theme and language): account preference (`GET /api/auth/me → prefs`) →
  `localStorage` → *(language only)* server default (`/api/settings/public → default_lang`) →
  *(language only)* browser language → built-in default (`system` / `tr`). A change made in the UI
  is applied immediately, written to `localStorage`, and, if there is a session, saved to the
  account with `PUT /api/me/prefs`.
- The light theme is **not an inverted dark theme**: status, accent and category colours have been
  re-chosen to give AA contrast on a white card; shadows are soft and neutral, with no "inner glow".

### Colour tokens (CSS custom properties / Tailwind `@theme`)

| Token | Dark | Light | Usage |
|-------|------|------|----------|
| `--color-bg` | `#0a0e1a` | `#f3f5fa` | app background |
| `--color-bg-elev` | `#0f1524` | `#fafbfd` | sidebar, tab strip |
| `--color-surface` | `#141b2d` | `#ffffff` | card |
| `--color-surface-2` | `#1b2740` | `#eef2f8` | card hover, input field, selected tab |
| `--color-surface-3` | `#22304d` | `#e2e8f1` | secondary button hover |
| `--color-glass` | `rgba(255,255,255,.04)` | `rgba(15,23,42,.04)` | subtle fill |
| `--color-border` / `-2` | white 8% / 14% | navy 9% / 16% | divider / prominent border |
| `--color-wash` / `-2` / `-3` | white 5% / 9% / 15% | navy 4.5% / 8% / 20% | neutral badge, hover fill, off switch |
| `--color-track` | white 7% | navy 9% | gauge/progress track |
| `--color-hairline` | white 4% | navy 5% | heatmap cell line |
| `--color-scrim` | `rgba(4,7,15,.7)` | `rgba(15,23,42,.38)` | modal/drawer backdrop |
| `--color-code` | `#0b1120` | `#f1f4f9` | code block, recovery codes |
| `--color-text` | `#e6edf7` | `#0f172a` | primary text |
| `--color-text-muted` | `#9fb0c6` | `#475569` | secondary text |
| `--color-text-dim` | `#64748b` | `#5b6b82` | tertiary text, axis label |
| `--color-on-accent` | `#04121f` | `#ffffff` | text on the accent gradient |
| `--color-accent` / `-2` | `#38bdf8` / `#6366f1` | `#0369a1` / `#4f46e5` | brand accent (text/icon) |
| `--color-brand-1` / `-2` | `#22d3ee` / `#6366f1` | `#0891b2` / `#4f46e5` | logo gradient stops |
| `--color-ok` | `#34d399` | `#047857` | online / healthy |
| `--color-warn` | `#fbbf24` | `#92400e` | warning |
| `--color-crit` | `#f87171` | `#b91c1c` | critical |
| `--color-offline` | `#64748b` | `#64748b` | offline |
| `--color-info` | `#38bdf8` | `#0369a1` | info |

Theme-dependent custom variables that are not tokens (`:root`): `--gradient-accent`,
`--gradient-accent-soft`, `--gradient-glass`, `--glow-1/2` (background glow), `--selection-*`,
`--skeleton-shine`, `--scroll-thumb(-hover)`. Shadows: `--shadow-card`, `--shadow-pop`,
`--shadow-raised` (selected tab/segment), `--shadow-primary` (primary button).

### Program-category palette (dataviz — categorical)

| | Dark | Light |
|-|------|------|
| browser | `#38bdf8` | `#0284c7` |
| office | `#34d399` | `#059669` |
| dev | `#a78bfa` | `#7c3aed` |
| media | `#f472b6` | `#db2777` |
| comms | `#fbbf24` | `#d97706` |
| system | `#64748b` | `#64748b` |
| other | `#9fb0c6` | `#94a3b8` |

Metric series: CPU `--color-metric-cpu`, RAM `--color-metric-mem`, Net↑ `--color-metric-netup`,
Net↓ `--color-metric-netdown` (same family as the category palette). Chart scaffolding:
`--color-chart-grid`, `--color-chart-axis`, `--color-chart-cursor`, `--color-chart-band`.
Heatmap: single-hue sequential ramp `--color-heat-1…6` + empty cell `--color-heat-empty`
(in dark, a deep blue that sinks into the surface → bright; in light, pale blue → dark). Colour
**alone** never carries meaning — reinforce it with an icon/label.

### Colour on the JS side

`src/lib/theme.ts` exports only `var(--color-…)` references (`chart.*`, `toneColor`,
`HEAT_RAMP`); so do `statusColor` and the category colours in `lib/labels.ts`. SVG attributes
and inline styles use them directly, so no redraw is needed when the theme changes.

- Translucent tone: `tint(color, percent)` (`color-mix`) — `` `${hex}22` `` concatenation is
  **forbidden**.
- Recharts requires a real colour value for some props: chart components resolve the token with
  `useResolveColor()`. The resolver's identity changes with the theme; the chart repaints itself
  on a theme change.
- Exception: the QR code is black on a white background in both themes (for scanner apps).

## Language (i18n)

A small, typed, in-house layer: `src/i18n/`.

- Dictionaries: `tr.ts` (the source of the key tree) and `en.ts` (typed as `Shape<typeof tr>` —
  a missing/wrong key is a compile error). The parts live under `i18n/tr/*.ts`, `i18n/en/*.ts`.
- `const t = useT()` → `t('nav.devices')`, `t('common.machines', { count: 3 })`. `{name}`
  placeholders; numbers are grouped per language. Plural: `plural('{count} machine', '{count} machines')`
  — the `count` parameter selects the form via `Intl.PluralRules` (in Turkish most keys have a
  single form).
- Sentences containing formatting are kept as **a single piece** in the dictionary with
  `<Trans k="…" values tags />` (`<b>…</b>` markers); a sentence is never assembled from fragments
  in JSX.
- Non-React code (`lib/format.ts`, `lib/labels.ts`, hooks that produce toasts) uses `translate()`.
- Fixed values coming from the server (rule id, status, category, result code…) are translated via
  `lib/labels.ts`; an id that is not in the dictionary is shown in a readable form, not raw
  (`crash_storm` → "Crash Storm").
- API errors arrive as `{code, message, params}`: the text is built from the `errors.<code>` key
  (`lib/errors.ts → errorText`); for an unknown code, `message` is shown.
- Alert sentences are built from `rule` + `params` with the `lib/alertText.ts` templates; every
  rule has a bare form for missing parameters.
- Date/number: `lib/format.ts` uses `tr-TR` or `en-GB` (day-month-year, 24-hour) according to the
  active language; the timezone is always `Europe/Istanbul`. Percent: `%42` (tr) / `42%` (en). For
  case conversion and sorting use `initialOf`, `lowerText`, `compareText` — do not hard-code a
  locale at the call site (the Turkish dotted İ problem). CSS `uppercase` also works correctly via
  `<html lang>`.
- `<html lang>` and `<title>` ("Page · Tolikopa") are updated together with the language.
- **There is no user-visible literal text under `src/` outside the dictionaries** (except Windows'
  own UI names and commands).

### Capitalisation rule

| Surface | Türkçe | English |
|-------|--------|---------|
| Navigation, tab, page/card/dialog title, button, table header, field label, badge/status | Every Word Capitalised ("Alarm Eşikleri", "Panel Kullanıcıları") | Title Case ("Alert Thresholds") |
| Description, hint, help text, empty state, notification (toast), `aria-label` | sentence case | sentence case |

In titles, the Turkish conjunctions "ve, ile, veya, de, da" and the English words "a, the, and, or,
of, to, in, on, for, per" stay lowercase.

## Typography

- Font: **Inter** (bundled locally; no CDN). Headings use `-0.01em` letter-spacing.
- Scale: display 30/36, h1 24/32, h2 20/28, h3 16/24, body 14/20, small 12/16, mono 12.
- **Numeric metrics use `font-variant-numeric: tabular-nums`** (so they don't jump). Weight 600-700
  for large KPI figures.

## Spacing / radius / elevation

- Spacing is 4px-based (Tailwind default). Card padding 20-24px; grid gap 16-20px.
- Radii: `--radius-sm 10px`, `md 14px`, `lg 18px`, `xl 26px`, fully rounded badges.
- Card: `--color-surface` + 1px `--color-border` + `--shadow-card`; `--color-surface-2` on hover.
- Glass effect (top bar, modal, toast): `backdrop-filter: blur(14px)` + `--gradient-glass`.

## Motion — restrained and purposeful

`motion` (`import { motion } from "motion/react"`). Durations and easing:

```
--dur-fast 120ms · --dur-base 200ms · --dur-slow 380ms
easeOut  cubic-bezier(0.22, 1, 0.36, 1)      // entrance
spring   { type:"spring", stiffness:220, damping:26 }  // interaction/layout
```

Patterns:
- **Page/card entrance:** opacity 0→1 + y 8px→0, `--dur-base`, `stagger` 0.03s on cards.
- **KPI counters:** `count-up` to the value (motion `animate` + `useMotionValue`), `--dur-slow`.
- **Live pulse:** a light `scale`/`opacity` pulse (2s loop) for the online/active dot; a short
  "flash" highlight on the card when a WS update arrives.
- **Fleet reordering:** `layout` prop + spring (smooth when cards are reordered by status).
- **Ring gauge:** arc `strokeDashoffset` transition `--dur-slow` easeOut.
- **Route transition:** fade+slide with `AnimatePresence`, `--dur-base`. A language change also
  rebuilds the page with the same transition.
- `prefers-reduced-motion` → disable/shorten animations (mandatory): a CSS rule + a root
  `<MotionConfig reducedMotion="user">`.

## Component inventory

- **AppShell:** collapsible Sidebar on the left (logo + organisation name, nav, version at the
  bottom), TopBar on top (search, live status, user menu → My Profile / Sign Out). The content area
  has a max width.
- **Logo:** on the second line, `org_name` if set, otherwise the translated generic tagline
  ("IT Fleet Monitoring").
- **PrefControls:** `Segmented` (a segment control that looks like a radio group), `ThemePicker`,
  `LanguagePicker`.
- **StatDot / StatusBadge:** online/active/locked/offline — colour + icon + label.
- **KpiCard:** icon, title, large number (count-up), trend mini-sparkline.
- **MachineCard (dashboard):** hostname, user, three **RingGauge**s (CPU/RAM/Disk), open-alert
  badge, live pulse; click → machine details. `layout`-animated in the grid.
- **RingGauge:** SVG circular gauge, coloured by thresholds (ok/warn/crit).
- **TimeSeriesChart (Recharts):** range selector (1s/6s/24s/7g/30g — 1h/6h/24h/7d/30d), glass
  tooltip, area-fill gradient from zero.
- **CategoryDonut / StackedBar:** program-type breakdown (category palette).
- **ActivityHeatmap:** day × hour heatmap (sequential ramp tokens).
- **Timeline:** user/machine active-usage strips (horizontal, hour axis).
- **AlertList / AlertBadge:** severity-coloured, acknowledge button; sentence from `rule`+`params`.
- **DataTable:** sortable, paginated, row hover.
- **Modal / ConfirmDialog:** focus trap; every destructive operation sits behind a ConfirmDialog
  (checkbox, type-to-confirm, or `confirmDisabled`). `dismissible={false}` is only for content that
  is shown once (recovery codes).
- **TwoFactorSetup / RecoveryCodesPanel / QrCode:** QR + manual-entry key → 6-digit code →
  recovery codes (copy, download .txt, "I have saved them" confirmation).
- **Setup parts (`components/setup/`):** `ServerUrlForm`, `DomainConfigForm` (shared by the wizard
  and the Setup page; a field error from the server lands on the relevant field), `RequiredRights`,
  `ReadinessList`, `ArtifactInfo` (version + copyable SHA-256), `RunInstructions`, `CodeBlock`,
  `DownloadButton` (fetches the file with `fetch` first; a JSON error body is not saved as a file,
  a translated notification is shown instead).
- **SetupBanner (AppShell):** a dismissible strip for administrators when an administrator exists
  but setup is not complete.
- **PasswordRules:** a live rule list (icon + text) under the new-password field.
- **Toast:** bottom-right, for WS alert notifications and operation results.
- **EmptyState / Skeleton:** shimmer while loading.

## Accessibility

- Contrast AA (text ≥ 4.5:1) **in both themes**. Focus rings are visible (`--color-accent`).
- Colour together with shape/label. `prefers-reduced-motion` is supported.
- All interactions by keyboard; `aria-label` in the active language on icon buttons.
- Each option in the language picker is written in its own language and carries a `lang`
  attribute.

## Page layouts (summary)

Routes are language-independent short English names; the old Turkish paths (`/cihazlar`,
`/ayarlar?sekme=…` etc.) redirect to their new equivalents.

- **First-Run Setup wizard:** when there is no administrator yet (`needs_admin`), a full-screen,
  sidebar-less flow that replaces the login screen; language and theme picker from the first
  screen on. Steps: Welcome → Admin Account (setup code; then optional 2FA) → Server Address →
  Domain (+ Required Rights panel) → Download and Run (readiness check, SHA-256,
  `tolikopa-setup.exe`) → First PC (polls every few seconds). Progress bar on top; "Step n of 6" on
  narrow screens. The step is derived from server state (continues where it left off on reload).
  The panel **does not ask** for the domain password.
- **Sign In (`/login`):** brand panel + form; language and theme picker at the top right. If 2FA is
  on, a second step (app code or recovery code).
- **2FA Setup (mandatory):** a full-screen page without the sidebar while `mfa_enrollment_required`;
  nothing is loaded except the language/theme picker and Sign Out.
- **Overview (`/`):** 4 KpiCards on top → fleet trend, attention cards, event feed, busy hours.
- **Devices (`/devices`):** KPIs + filters + MachineCard grid.
- **Machine Details (`/machines/:id`):** identity + live RingGauges → tabs: Metrics / Apps /
  Categories / Sessions / Alerts / Remote Management.
- **Slow Machines (`/slow-machines`)**, **Users (`/users`, `/users/:user`)**,
  **Departments (`/departments`)**, **Alerts (`/alerts`)**.
- **Settings (`/settings?tab=…`):** General (everyone: theme + language; administrator:
  organisation name, default language, 2FA requirement) · Categories · Apps to Remove · Machines ·
  Alert Thresholds · Panel Users (role, status, 2FA, last login; edit / disable / reset password /
  reset 2FA / delete).
- **Profile (`/profile`):** account information, password change, two-step verification, personal
  preferences, session + version.
- **Setup (`/setup`, administrators only):** status (server address, masked ingest token, agent
  build, fleet by version) → Agent Update card (`tolikopa-update.exe`) → server address and domain
  forms + Required Rights → re-download the setup tool → collapsed "Advanced" (individual files,
  manual GPO steps; paths are built from the configured FQDN) → Maintenance (uninstall, rerun the
  wizard — behind a ConfirmDialog).
