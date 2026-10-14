//go:build windows

package collector

import (
	"fmt"
	"log/slog"
	"runtime"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows/registry"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// updateQueryTimeout bounds the whole WUA COM interaction so a wedged Windows
// Update provider can never stall the agent. The pending-count search is a
// LOCAL-CACHE query (Online=false), so it is normally fast; the timeout is
// belt-and-suspenders.
const updateQueryTimeout = 60 * time.Second

// Registry fallback locations for the last-success timestamps (present mainly on
// older Windows; modern builds expose these via the AutoUpdate COM object).
const (
	regWURebootRequired = `SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`
	regWUDetectResults  = `SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\Results\Detect`
	regWUInstallResults = `SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\Results\Install`
)

type winUpdates struct {
	log *slog.Logger
}

// NewUpdateCollector returns the Windows WUA+registry update-posture collector.
func NewUpdateCollector(_ *config.Config, log *slog.Logger) UpdateCollector {
	return &winUpdates{log: log}
}

// Collect gathers the machine's Windows Update posture. It NEVER panics and
// always returns a value: a fast COM-free reboot-pending check plus a best-effort
// pending count and last scan/install dates read from Windows' own assessment.
// Any failure degrades gracefully (PendingCount stays -1 = unknown, dates zero).
func (w *winUpdates) Collect() *model.UpdateStatus {
	us := &model.UpdateStatus{
		PendingCount: -1,
		CheckedAt:    time.Now().UTC(),
	}

	com := w.comCollect()
	us.PendingCount = com.pending
	us.LastSearch = com.lastSearch
	us.LastInstall = com.lastInstall

	// Reboot: prefer the authoritative WUA SystemInfo.RebootRequired flag; fall back
	// to the Windows Update "RebootRequired" registry key. The Component-Based-
	// Servicing "RebootPending" key is deliberately NOT used — it is set by any
	// component servicing and often lingers after a reboot, which produced false
	// "restart needed" reports.
	if com.rebootKnown {
		us.RebootPending = com.reboot
	} else {
		us.RebootPending = keyExists(regWURebootRequired)
	}

	// Registry fallback for systems where the COM dates are unavailable.
	if us.LastSearch.IsZero() {
		us.LastSearch = wuLastSuccess(regWUDetectResults)
	}
	if us.LastInstall.IsZero() {
		us.LastInstall = wuLastSuccess(regWUInstallResults)
	}
	return us
}

func keyExists(path string) bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	_ = k.Close()
	return true
}

// wuLastSuccess reads the "LastSuccessTime" value (REG_SZ "2006-01-02 15:04:05",
// local time) written by older Windows Update agents under the given results key.
func wuLastSuccess(path string) time.Time {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return time.Time{}
	}
	defer k.Close()
	s, _, err := k.GetStringValue("LastSuccessTime")
	if err != nil || s == "" {
		return time.Time{}
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// comResult bundles the values gathered from the Windows Update COM API.
type comResult struct {
	pending     int // -1 = unknown
	lastSearch  time.Time
	lastInstall time.Time
	rebootKnown bool // whether SystemInfo.RebootRequired could be read
	reboot      bool
}

// comCollect drives the Windows Update COM API on a dedicated OS-locked thread,
// bounded by a timeout and wrapped in a recover, so a misbehaving update stack
// can never destabilise the agent (which runs as SYSTEM on every machine). It
// reads only COUNTS and TIMESTAMPS — never update titles or history.
func (w *winUpdates) comCollect() comResult {
	ch := make(chan comResult, 1)
	go func() {
		res := comResult{pending: -1}
		defer func() {
			if r := recover(); r != nil {
				w.log.Warn("wua com panicked", "recover", fmt.Sprint(r))
			}
			select {
			case ch <- res:
			default:
			}
		}()
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
			w.log.Debug("wua CoInitializeEx failed", "err", err.Error())
			return
		}
		defer ole.CoUninitialize()

		// Last scan/install dates from Windows' own auto-update results (fast; no scan).
		res.lastSearch, res.lastInstall = wuaAutoUpdateDates()

		// Authoritative "reboot required to finish updates" flag.
		if rb, ok := wuaSystemInfoReboot(); ok {
			res.reboot, res.rebootKnown = rb, true
		}

		// Pending count from the LOCAL cache (Online=false) — reads Windows' existing
		// assessment instead of initiating a WSUS/WU scan from the agent.
		if n, err := wuaLocalPendingCount(); err == nil {
			res.pending = n
		} else {
			w.log.Debug("wua local search failed", "err", err.Error())
		}
	}()

	select {
	case r := <-ch:
		return r
	case <-time.After(updateQueryTimeout):
		w.log.Warn("wua com timed out")
		return comResult{pending: -1}
	}
}

// wuaAutoUpdateDates reads Microsoft.Update.AutoUpdate.Results for the last
// successful search/install dates. Returns zero times when unavailable.
func wuaAutoUpdateDates() (lastSearch, lastInstall time.Time) {
	unknown, err := oleutil.CreateObject("Microsoft.Update.AutoUpdate")
	if err != nil {
		return
	}
	defer unknown.Release()
	au, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return
	}
	defer au.Release()

	resultsV, err := oleutil.GetProperty(au, "Results")
	if err != nil {
		return
	}
	results := resultsV.ToIDispatch()
	defer results.Release()

	return comDate(results, "LastSearchSuccessDate"), comDate(results, "LastInstallationSuccessDate")
}

// comDate reads a VT_DATE property and normalises it to UTC. Empty/never dates
// (OLE epoch ~1899) map to the zero time.
func comDate(disp *ole.IDispatch, prop string) time.Time {
	v, err := oleutil.GetProperty(disp, prop)
	if err != nil {
		return time.Time{}
	}
	defer v.Clear()
	if t, ok := v.Value().(time.Time); ok && t.Year() >= 2000 {
		return t.UTC()
	}
	return time.Time{}
}

// wuaSystemInfoReboot reads Microsoft.Update.SystemInfo.RebootRequired — the
// authoritative "a reboot is required to finish installing updates" flag. Returns
// (value, ok); ok=false when the object/property is unavailable.
func wuaSystemInfoReboot() (rebootRequired, ok bool) {
	unknown, err := oleutil.CreateObject("Microsoft.Update.SystemInfo")
	if err != nil {
		return false, false
	}
	defer unknown.Release()
	si, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return false, false
	}
	defer si.Release()
	v, err := oleutil.GetProperty(si, "RebootRequired")
	if err != nil {
		return false, false
	}
	defer v.Clear()
	if b, isBool := v.Value().(bool); isBool {
		return b, true
	}
	return false, false
}

// wuaLocalPendingCount counts the not-installed software updates that Windows
// would actually offer/install — from the LOCAL WUA cache (Online=false). It
// EXCLUDES browse-only (optional/preview) updates, which Windows Update does not
// treat as pending, so the count matches the "You're up to date" experience
// instead of over-reporting optional items. Uses only error-returning oleutil
// calls and releases every COM object.
func wuaLocalPendingCount() (int, error) {
	unknown, err := oleutil.CreateObject("Microsoft.Update.Session")
	if err != nil {
		return 0, err
	}
	defer unknown.Release()

	session, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return 0, err
	}
	defer session.Release()

	searcherV, err := oleutil.CallMethod(session, "CreateUpdateSearcher")
	if err != nil {
		return 0, err
	}
	searcher := searcherV.ToIDispatch()
	defer searcher.Release()

	if _, err := oleutil.PutProperty(searcher, "Online", false); err != nil {
		_ = err // non-fatal; fall back to the default
	}

	resultV, err := oleutil.CallMethod(searcher, "Search", "IsInstalled=0 AND IsHidden=0 AND Type='Software'")
	if err != nil {
		return 0, err
	}
	result := resultV.ToIDispatch()
	defer result.Release()

	updatesV, err := oleutil.GetProperty(result, "Updates")
	if err != nil {
		return 0, err
	}
	updates := updatesV.ToIDispatch()
	defer updates.Release()

	countV, err := oleutil.GetProperty(updates, "Count")
	if err != nil {
		return 0, err
	}
	total := int(countV.Val)

	// Count only non-browse-only updates (i.e. what Windows would auto-offer).
	pending := 0
	for i := 0; i < total; i++ {
		itemV, err := oleutil.GetProperty(updates, "Item", i)
		if err != nil {
			continue
		}
		item := itemV.ToIDispatch()
		browse := false
		if bv, err := oleutil.GetProperty(item, "BrowseOnly"); err == nil {
			if b, isBool := bv.Value().(bool); isBool {
				browse = b
			}
			bv.Clear()
		}
		item.Release()
		if !browse {
			pending++
		}
	}
	return pending, nil
}
