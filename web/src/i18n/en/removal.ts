// English — endpoint hygiene (app removal): enum labels, settings tab, panels.
import { plural } from '../plural'
import type { Shape } from '../plural'
import type * as tr from '../tr/removal'

export const removal: Shape<typeof tr.removal> = {
  mode: {
    off: { label: 'Off', description: 'Agents evaluate nothing and touch nothing.' },
    dry_run: { label: 'Dry Run', description: 'Agents report what they would do and remove nothing.' },
    enforce: {
      label: 'Enforce',
      description: 'Selected apps are removed and selected startup entries are disabled.',
    },
  },
  risk: {
    safe: { label: 'Safe', description: 'No business use; removing it has no expected impact.' },
    review: { label: 'Review', description: 'Can be removed, but some staff may use it. Your call.' },
    advanced: { label: 'Advanced', description: 'May break Windows behaviour. Try it on a single machine first.' },
  },
  kind: { app: 'App', startup: 'Startup Entry' },
  kindPlural: { app: 'Apps', startup: 'Startup Entries' },
  category: {
    oyun: 'Games',
    medya: 'Media',
    uretkenlik: 'Productivity',
    iletisim: 'Communication',
    sistem: 'System',
    oem: 'Manufacturer Preinstall (OEM)',
    other: 'Other',
  },
  scope: {
    user: 'Existing user profiles',
    provisioned: 'New profiles (image)',
    both: 'Existing and new profiles',
  },
  action: {
    remove: 'Remove',
    deprovision: 'Deprovision',
    disable: 'Disable',
    enable: 'Enable',
  },
  outcome: {
    ok: { label: 'Succeeded', description: 'The action was applied on the machine.' },
    dry_run: { label: 'Dry Run', description: 'Dry-run mode — the agent reported what it would do and touched nothing.' },
    not_present: { label: 'Not Present', description: 'The item was not found on this machine; nothing to do.' },
    not_allowed: { label: 'No GPO Permission', description: 'The local kill switch on the machine has this kind turned off.' },
    blocked: {
      label: 'Blocked by Windows',
      description: 'Windows refused the removal — a protected package or a deployment problem on the machine.',
    },
    failed: { label: 'Failed', description: 'The action returned an error code.' },
    timeout: { label: 'Timed Out', description: 'The action did not finish within the time limit.' },
    unknown_item: { label: 'Unknown Item', description: 'The agent’s catalog does not have this item — the agent is out of date.' },
  },
  error: {
    '0X80073D02': {
      label: 'App Was Running',
      hint: 'The app or one of its components was running during removal. It usually resolves by itself on the next pass once the user signs out or the computer restarts.',
    },
    '0X80073D19': {
      label: 'User Signed Out',
      hint: 'The user the package is registered for signed out while the action was running. It is retried on the next pass.',
    },
    '0X80073CFA': {
      label: 'Windows Refused the Removal',
      hint: 'The generic “removal failed” code. If it shows for one app only, that package is protected on this Windows version; take it out of the selection. If it shows for every app on the same machine, the problem is the machine: check the AppXDeploymentServer event log or exclude the machine by tag.',
    },
    '0X80070005': {
      label: 'Access Denied',
      hint: 'Denied even though the agent runs as SYSTEM — usually Microsoft Store was updating the same package at that moment. If it persists, check Store updates on the machine.',
    },
    '0X80073CF1': {
      label: 'Package Not Found',
      hint: 'The version to be removed is no longer registered (it may have been updated in the meantime). The current version is resolved again on the next pass.',
    },
    '0X80073CF6': {
      label: 'Registration Failed',
      hint: 'Windows could not update the package registration. If it persists, check the AppXDeploymentServer event log on the machine.',
    },
    '0X80073CF9': {
      label: 'Deployment Failed',
      hint: 'The Windows deployment engine could not finish the action. If it persists after a restart, inspect the machine by hand.',
    },
    '0X80073CFE': {
      label: 'Package Store Corrupted',
      hint: 'The machine’s Appx package store looks corrupted; this is unrelated to removal — the machine needs repairing.',
    },
    '0X800704C7': {
      label: 'Cancelled',
      hint: 'Windows cancelled the action (shutdown or restart). It is retried on the next pass.',
    },
    '0X80004002': {
      label: 'Windows Version Not Supported',
      hint: 'Windows on this machine does not offer the required deployment interface (before 1809). Exclude the machine by tag.',
    },
    '0X80040154': {
      label: 'Deployment Component Missing',
      hint: 'The PackageManager component is not registered — usually Server Core or a stripped-down Windows install.',
    },
    '0X80070002': {
      label: 'Registry Value Missing',
      hint: 'The startup entry was deleted in the meantime. It closes as “not present” at the next inventory.',
    },
    '0XE0000001': {
      label: 'Agent: WinRT Table Missing',
      hint: 'The removal plumbing is not verified in this agent version. Upgrade the agent to the current version.',
    },
    '0XE0000002': {
      label: 'Agent: Allowlist Refused',
      hint: 'The removal worker did not find the package in its own allowlist — agent and catalog disagree. Update the agent.',
    },
    '0XE0000003': {
      label: 'Agent: No Approval Key',
      hint: 'The startup entry has no StartupApproved counterpart, so it cannot be disabled (RunOnce entries).',
    },
    '0XE0000004': {
      label: 'Agent: Removal Worker Did Not Start',
      hint: 'The agent could not start, or could not talk to, the separate removal process. If it persists, restart the agent service.',
    },
    '0XE0000005': {
      label: 'Agent: Package List Unreadable',
      hint: 'The agent could not ask Windows which packages are installed; it stopped rather than act blind. It is retried on the next pass.',
    },
  },
  presence: {
    unknown: 'number of machines with it installed is unknown',
    startupNone: 'not active on any machine',
    startupSome: plural('active on {count} machine', 'active on {count} machines'),
    appNone: 'not installed on any machine',
    appSome: plural('installed on {count} machine', 'installed on {count} machines'),
    startupUnit: plural('machine has it active', 'machines have it active'),
    appUnit: plural('machine has it installed', 'machines have it installed'),
    reported: plural('{count} machine reported on this item', '{count} machines reported on this item'),
  },

  section: {
    blockNoGpo:
      'No machine has the GPO permission turned on. No dry-run result will arrive until allow_app_removal / allow_startup_control is enabled in config.json and deployed.',
    blockWaiting:
      'Dry-run mode is saved, but no result has arrived yet. Machines with the GPO permission send their first result at the next hygiene pass (hygiene_hours at the latest).',
    blockNeedDryRun: 'Save in dry-run mode first and wait until at least one result arrives.',
    blockSelectionChanged: 'You changed the selection — save in dry-run mode first and look at the result.',
    saved: 'Policy saved',
    savedEnforce: plural(
      'Enforce mode is on — {count} machine in scope. Agents start acting at their next report.',
      'Enforce mode is on — {count} machines in scope. Agents start acting at their next report.',
    ),
    savedMode: 'Operating mode: {mode}.',
    saveFailed: 'Could not save the policy',
    notDeployedTitle: 'App removal service is not available',
    loadFailedTitle: 'Could not load the catalog',
    notDeployedBody:
      'The version on this server does not include the app removal API yet. This tab fills in by itself once the server is updated.',
    loadFailedBody: 'Could not fetch the catalog and policy. Try refreshing the page.',
    liveEnforce: 'Saved operating mode: enforce. {machines} machines in scope; the {apps} selected apps are being removed.',
    liveMode: 'Saved operating mode: {mode}.',
    intro:
      'The panel can only select from the list compiled into the agent; adding anything to that list takes a new agent build. On each machine the local permission granted by GPO has the final say — when it is off, the selection here does nothing on that machine.',
    stale:
      'The policy was updated in another session. Your edits are kept; if you load the server version, your unsaved changes are lost.',
    loadServer: 'Load Server Version',
    unsavedTitle: 'Unsaved Changes',
    unsavedSummary: 'Selected: {apps} apps, {startup} startup entries.',
    saveAndEnforce: 'Save and Enforce',
    tabImpact: 'Impact of Removal',
    tabFailures: 'Open Failures',
    tabLog: 'Action Log',
    resultsTabs: 'Removal results',
    confirmTitle: 'Start Fleet-Wide Removal',
    confirmLabel: 'Start Enforcing',
    confirmBody:
      'When you save, <b>{machines}</b> machines come into scope and the {apps} selected apps are removed from them. Removed apps cannot be reinstalled from the panel.',
    confirmType: 'To confirm, type the number of machines in scope',
    confirmHintOnline: plural(
      '{count} machine is online right now; the rest apply the policy when they power on.',
      '{count} machines are online right now; the rest apply the policy when they power on.',
    ),
    confirmHintOffline: 'Offline machines apply this policy too, as soon as they power on.',
    confirmAck: 'I understand that removal cannot be undone and may affect users.',
    confirmStartup: plural(
      '{count} startup entry is only disabled; the registry value is kept and it can be re-enabled later.',
      '{count} startup entries are only disabled; the registry values are kept and they can be re-enabled later.',
    ),
    confirmKilled: plural(
      '{count} machine has the GPO permission off — nothing will be applied there.',
      '{count} machines have the GPO permission off — nothing will be applied there.',
    ),
    confirmDrift: plural(
      '{count} machine has an outdated agent catalog — new items will come back as “unknown item”.',
      '{count} machines have an outdated agent catalog — new items will come back as “unknown item”.',
    ),
  },

  attempts: {
    allOutcomes: 'All Outcomes',
    item: 'Item',
    outcomes: 'Outcomes',
    lastAction: 'Last Action',
    time: 'Time',
    machineDeleted: 'Deleted',
    machineDeletedTitle: 'This machine was permanently deleted; the attempt is kept as an audit trail.',
    action: 'Action',
    outcome: 'Outcome',
    errorCode: 'Error Code',
    healedTitle: 'After this failure the item was removed from the machine or is no longer there.',
    healed: 'Resolved Later',
    byItemTitle: 'Results by Item',
    byItemDryRun: plural(
      '{count} logged action was produced in dry-run mode — it changed nothing on the machines.',
      '{count} logged actions were produced in dry-run mode — they changed nothing on the machines.',
    ),
    byItemBody: 'Per-item summary of the actions agents have reported.',
    emptyTitle: 'No actions logged yet',
    emptyBody: 'After the policy is saved, agents write their results here at their next report.',
    recentTitle: 'Recent Actions',
    recentBody: 'The audit trail reported by agents. Error codes are codes; they carry no OS message or file path.',
    filterLabel: 'Filter by outcome',
    noRecords: 'No records found',
  },

  catalog: {
    safeDefault: 'Safe Default',
    deprecated: 'Removed From Catalog',
    title: 'Catalog',
    body: 'The allowlist compiled into the agent. The panel can only select from this list; it cannot send anything else.',
    selectSafe: 'Select Safe Defaults ({count})',
    clear: 'Clear Selection',
    kindGroup: 'Item kind',
    kindChipTitle: '{kind} — number selected',
    appSummary: '{total} apps · {selected} selected',
    startupSummary: '{total} startup entries · {selected} selected',
    searchPlaceholder: 'Search apps, packages or processes…',
    searchLabel: 'Search the catalog',
    riskGroup: 'Risk level filter',
    allRisks: 'All Risks',
    onlySelected: 'Selected Only',
    onlyPresentTitle: 'Only items actually present in the fleet',
    onlyPresent: 'Installed Only',
    noMatchTitle: 'No matching items',
    noMatchBody: 'Change the search and filters and try again.',
    selectAllIn: 'All — select the items in the {category} category',
    footnote:
      'The selection is kept in two lists: apps are removed, startup entries are only disabled (not deleted). Switching between the tabs does not reset the selection.',
  },

  failures: {
    staleAgentLabel: 'Agent Out of Date',
    staleAgentHint:
      'The agent on this machine is behind the current version; the failure most likely comes from the old agent and will not clear by itself. Update the agent via GPO first.',
    staleAgentHintVersion:
      'The agent on this machine is behind the current version ({version}); the failure most likely comes from the old agent and will not clear by itself. Update the agent via GPO first.',
    timeoutLabel: 'Timed Out',
    timeoutHint:
      'The removal did not finish within the time limit — usually the Store was updating at that moment or the machine was busy. It is retried on the next pass.',
    unknownItemLabel: 'Agent Catalog Out of Date',
    unknownItemHint: 'The agent on this machine does not know the item. Upgrade the agent to the current version via GPO.',
    blockedLabel: 'Blocked by Windows',
    blockedHint:
      'Windows refused the removal. If it is one app, the package is protected — take it out of the selection; if it is every app on the machine, inspect the machine.',
    unknownCodeLabel: 'Unrecognised Code',
    noCodeLabel: 'Failure Without a Code',
    unknownCodeHint:
      'There is no recorded explanation for this code. Look for the “hygiene action” line with the same timestamp in the agent log on the machine.',
    item: 'Item',
    itemCount: plural('{count} item', '{count} items'),
    reason: 'Reason',
    expectation: 'Outlook',
    selfHealing: 'May Resolve Itself',
    needsAction: 'Needs Attention',
    lastAttempt: 'Last Attempt',
    title: 'Open Failures',
    summary:
      'Of the {total} machine–item pairs that failed in the last 90 days, {resolved} resolved later (removed on a later pass, or no longer on the machine). Only the {open} still open are listed below; failures open on the same machines for the same reason are grouped into one row.',
    body: 'Records whose last attempt failed and whose item is still on the machine.',
    loadFailedTitle: 'Could not load the failure summary',
    notSupported: 'The server may not support this view yet; it fills in once it is updated.',
    noneTitle: 'No open failures',
    noneAllResolved: plural(
      'The {count} failure in the log resolved later.',
      'All {count} failures in the log resolved later.',
    ),
    noneBody: 'No machine has a failed removal pending.',
  },

  impact: {
    windowOption: plural('{count} day before / after', '{count} days before / after'),
    points: '{value} pts',
    appsOn: plural('{count} app · {date}', '{count} apps · {date}'),
    title: 'Impact of Removal on Usage',
    body: 'Average CPU and RAM usage of machines with a successful removal, in the period before and after it. Only hours with an active session are counted.',
    windowLabel: 'Comparison window',
    loadFailedTitle: 'Could not load impact data',
    noneTitle: 'No successful removals yet',
    noneBody: 'No app has been successfully removed on any machine in the last 90 days; there is nothing to compare yet.',
    avgCpu: 'Average CPU',
    avgRam: 'Average RAM',
    coverage: 'Coverage',
    coverageBody:
      'Of the {treated} machines with a removal, {included} have at least {hours} active hours of data on both sides; the average is taken over those. Control group: {control} machines.',
    netNote:
      '<b>Net impact</b> is the change on machines with a removal minus the change, over the same period, on machines where nothing was removed — so fleet-wide swings such as a holiday week or a Windows update are not credited to the removal. The measurement is machine-wide; it may include other changes from the same period.',
    incomplete: plural(
      'On {count} machine the “after” window is not complete yet; these values are not final.',
      'On {count} machines the “after” window is not complete yet; these values are not final.',
    ),
    after: 'after',
    treated: 'With Removal',
    control: 'Control Group',
    net: 'Net Impact',
    app: 'App',
    removedFrom: 'Machines Removed From',
    seenBefore: 'Seen Running Before',
    ownCpu: 'Own CPU',
    ownMem: 'Own Memory',
    afterCol: 'After',
    cameBackTitle: 'This app’s process was seen again after the removal — it may have been reinstalled.',
    cameBack: plural('Back on {count} machine', 'Back on {count} machines'),
    notSeen: 'Not Seen',
    perAppTitle: 'Direct Savings by App',
    perAppBody:
      'Average CPU and memory usage of the removed app’s own processes while it was running before removal. The agent sends only the busiest processes in each sample, so these values are a lower bound; “—” means the app never stood out.',
    activeHours: 'Active Hours',
    hoursBeforeAfter: '{before} before · {after} after',
    windowOpen: 'Window Still Open',
    perMachineTitle: 'By Machine',
    perMachineBody: 'Sides with fewer than {hours} active hours are shown in orange and left out of the fleet average.',
  },

  modeControl: {
    title: 'Operating Mode',
    body: 'Decides what agents do with this policy. Dry run is the default.',
    saved: 'Saved: {mode}',
    enforceWarning:
      'Once you save, this cannot be undone: the selected apps will be removed from the targeted machines. Startup entries are only disabled and can be re-enabled later.',
    lastDryRun: 'Last dry-run result: <b>{when}</b>',
    liveEnforce: 'Operating mode set to enforce. When saved, the selected apps will be removed from the machines.',
    liveMode: 'Operating mode set to {mode}.',
  },

  diff: {
    none: 'No unsaved changes.',
    mode: 'Operating Mode',
    added: 'Added to the list ({count})',
    removed: 'Removed from the list ({count})',
    tags: 'Excluded tags:',
  },

  summary: {
    targeted: 'Machines in Scope',
    targetedHint: '{excluded} of {fleet} Windows machines excluded by tag · {online} online',
    fleetLoading: 'Loading the fleet list',
    reporting: 'Reporting Hygiene Status',
    reportingHint: plural(
      '{count} machine has applied the saved policy revision.',
      '{count} machines have applied the saved policy revision.',
    ),
    drift: 'Agent Catalog Out of Date',
    driftHint: plural(
      '{count} machine has an outdated agent catalog — it does not know new items and returns “unknown item”. Update the agent via GPO.',
      '{count} machines have an outdated agent catalog — they do not know new items and return “unknown item”. Update the agent via GPO.',
    ),
    driftNone: 'All agents are on the same catalog version as the server.',
    killed: 'GPO Permission Off',
    killedHint: plural(
      '{count} machine has the GPO permission off — while the local kill switch is off, nothing is applied whatever is selected here.',
      '{count} machines have the GPO permission off — while the local kill switch is off, nothing is applied whatever is selected here.',
    ),
    killedNone: 'The local permission is on for every machine.',
    silentNote:
      'On these machines the policy silently does nothing. When you assess results, read the <b>“No GPO Permission”</b> and <b>“Unknown Item”</b> outcomes together with these two numbers.',
  },

  tags: {
    title: 'Excluded Tags',
    body: 'No action is applied to machines carrying any of these tags.',
    excludedCount: plural('{count} machine excluded', '{count} machines excluded'),
    selected: 'Selected',
    noneSelected: 'No tag is excluded — the policy covers every Windows machine.',
    unusedTag: 'No machine carries this tag at the moment',
    stopExcluding: 'Stop excluding the tag {tag}',
    fleetTags: 'Tags in the Fleet',
    fleetTagsGroup: 'Machine tags in the fleet',
    exclude: plural('Exclude the tag {tag} ({count} machine)', 'Exclude the tag {tag} ({count} machines)'),
    addPlaceholder: 'Add a tag that is not in the list',
    addLabel: 'Add a tag to exclude',
    footnote:
      'The tag list is derived from the fleet view (there is no separate tag service), so it only shows tags used on at least one machine. A tag you type by hand is saved as well; if no machine carries it today, it excludes nobody.',
  },
}
