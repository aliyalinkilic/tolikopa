package appcatalog

import "strings"

// GENERATED-BY-HAND COPY of agent/internal/appcatalog/forbidden.go. The agent is
// the enforcement point; this copy is defence in depth so that a catalog naming a
// protected package is rejected at server startup too, instead of being mirrored
// into remediation_item and OFFERED IN THE PANEL while every agent panics on load.
// Keep the two in sync — `bash deploy/check.sh` runs both packages' tests.
//
// Removing anything here ranges from "user cannot log in" (AAD.BrokerPlugin) to
// "half the installed apps stop launching" (VCLibs, WindowsAppRuntime, WebView2).
// Windows itself protects some of them and will return 0x80073CFA, but we must not
// rely on that: on older builds several ARE removable and the damage is permanent
// short of a reinstall.
//
// Prefix rules are preferred over exact names because runtime packages carry
// version-stamped identities (Microsoft.VCLibs.140.00_..., Microsoft.UI.Xaml.2.8_...).

// forbiddenPrefixes match the start of a PackageFamilyName, case-insensitively.
var forbiddenPrefixes = []string{
	// ── Runtimes and frameworks other applications link against ──────────────
	"microsoft.vclibs.",
	"microsoft.net.native.",
	"microsoft.ui.xaml.",
	"microsoft.windowsappruntime.",
	"microsoft.services.store.engagement",
	"microsoft.webpimageextension",
	"microsoft.heifimageextension",
	"microsoft.vp9videoextensions",
	"microsoft.hevcvideoextension",

	// ── Edge / WebView2. WebView2 is what makes the inbox Weather app heavy, but
	//    it is also the render host for Widgets, Outlook, Teams and much of the
	//    shell. Removing it is not a performance fix, it is an outage. ─────────
	"microsoft.microsoftedge",
	"microsoft.edge",
	"microsoft.win32webviewhost",
	"microsoft.webview",

	// ── Identity, credentials, lock screen: removing these can lock every domain
	//    user out of the machine. ───────────────────────────────────────────────
	"microsoft.aad.brokerplugin",
	"microsoft.accountscontrol",
	"microsoft.creddialoghost",
	"microsoft.lockapp",
	"windows.cloudexperiencehost",
	"microsoft.windows.cloudexperiencehost",

	// ── Shell surfaces. Without these there is no Start menu, no search box and
	//    no notification area. ─────────────────────────────────────────────────
	"microsoft.windows.shellexperiencehost",
	"microsoft.windows.startmenuexperiencehost",
	"microsoft.windows.search",
	"microsoft.windows.contentdeliverymanager",
	"microsoft.windows.peopleexperiencehost",
	"microsoft.windows.apprep.chxapp",
	"microsoft.windows.assignedaccesslockapp",
	"microsoft.windows.callingshellapp",
	"microsoft.windows.oobenetworkcaptiveportal",
	"microsoft.windows.oobenetworkconnectionflow",
	"microsoft.windows.parentalcontrols",
	"microsoft.windows.pinningconfirmationdialog",
	"microsoft.windows.printqueueactioncenter",
	"microsoft.windows.secureassessmentbrowser",
	"microsoft.windows.xgpuejectdialog",
	"microsoft.xboxgamecallableui",
	"microsoft.asyncTextService",
	"microsoft.ecapp",
	"windows.cbspreview",
	"microsoft.windows.narratorquickstart",

	// ── Security and management ───────────────────────────────────────────────
	"microsoft.sechealthui",
	"microsoft.windows.securityhealth",
	"microsoft.desktopappinstaller", // winget: the tool IT uses to fix everything else
	"microsoft.windowsstore",        // without the Store nothing can be reinstalled
	"microsoft.storepurchaseapp",
	"microsoft.windowsterminal",
	"microsoft.wincompatibility",
	"microsoft.applicationcompatibility",
}

// forbiddenExact matches a whole PackageFamilyName, case-insensitively. Used where
// a prefix would be too greedy.
var forbiddenExact = map[string]struct{}{
	"microsoft.windows.photos_8wekyb3d8bbwe":       {}, // no longer removable cleanly on 24H2+; users depend on it
	"microsoft.windowsstore_8wekyb3d8bbwe":         {},
	"microsoft.windowsnotepad_8wekyb3d8bbwe":       {},
	"microsoft.paint_8wekyb3d8bbwe":                {},
	"microsoft.windowscalculator_8wekyb3d8bbwe":    {},
	"microsoft.screensketch_8wekyb3d8bbwe":         {}, // Snipping Tool — also the PrtSc handler
	"microsoft.windowscamera_8wekyb3d8bbwe":        {},
	"microsoft.hostedexperiences_8wekyb3d8bbwe":    {},
}

// IsForbiddenFamily reports whether a PackageFamilyName must never be removed.
// Matching is case-insensitive; unknown/empty input is treated as forbidden so a
// parsing slip can never widen the blast radius.
func IsForbiddenFamily(family string) bool {
	f := strings.ToLower(strings.TrimSpace(family))
	if f == "" {
		return true
	}
	if _, ok := forbiddenExact[f]; ok {
		return true
	}
	for _, p := range forbiddenPrefixes {
		if strings.HasPrefix(f, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
