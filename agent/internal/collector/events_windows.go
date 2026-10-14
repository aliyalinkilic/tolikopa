//go:build windows

package collector

import (
	"encoding/xml"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// EvtQuery / EvtRender flags (winevt.h).
const (
	evtQueryChannelPath         = 0x1
	evtQueryForwardDirection    = 0x100
	evtQueryTolerateQueryErrors = 0x1000
	evtRenderEventXml           = 1
	errorNoMoreItems            = 259
	errorInsufficientBuffer     = 122
	eventsPerPoll               = 16
	maxEventsPerPoll            = 500
	eventLookbackMS             = 86400000 // 24h in ms for the first-poll query bound
	bookmarkFile                = "events_bookmark"
)

// Application-channel XPath: only crash (1000, Application Error) and hang
// (1002, Application Hang) within the lookback window. This is the ONLY event
// data the agent ever fetches.
var eventQuery = "*[System[Provider[@Name='Application Error' or @Name='Application Hang']" +
	" and (EventID=1000 or EventID=1002)" +
	" and TimeCreated[timediff(@SystemTime) <= " + strconv.Itoa(eventLookbackMS) + "]]]"

var (
	modWevtapi    = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtQuery  = modWevtapi.NewProc("EvtQuery")
	procEvtNext   = modWevtapi.NewProc("EvtNext")
	procEvtRender = modWevtapi.NewProc("EvtRender")
	procEvtClose  = modWevtapi.NewProc("EvtClose")
)

var exceptionRe = regexp.MustCompile(`^0x[0-9a-fA-F]{1,8}$`)

// Minimal XML shapes. We deliberately bind ONLY Provider name, EventID,
// TimeCreated, EventRecordID and the ordered EventData/Data values, and from
// Data we copy ONLY the app base name and (for crashes) the exception code.
// The message string and any file paths are never extracted. See docs/AGENT.md.
type evtID struct {
	Value int `xml:",chardata"`
}
type evtSystem struct {
	Provider struct {
		Name string `xml:"Name,attr"`
	} `xml:"Provider"`
	EventID     evtID `xml:"EventID"`
	TimeCreated struct {
		SystemTime string `xml:"SystemTime,attr"`
	} `xml:"TimeCreated"`
	EventRecordID uint64 `xml:"EventRecordID"`
}
type evtXML struct {
	System    evtSystem `xml:"System"`
	EventData struct {
		Data []string `xml:"Data"`
	} `xml:"EventData"`
}

type winEvents struct {
	log        *slog.Logger
	bookmark   string
	lastRecord uint64
	baselined  bool // true once the first poll has established a baseline
}

// NewEventCollector returns the Windows wevtapi crash/hang collector. It resumes
// from the persisted record bookmark, so a restart neither replays nor drops.
func NewEventCollector(log *slog.Logger) EventCollector {
	e := &winEvents{
		log:      log,
		bookmark: filepath.Join(config.StateDir(), bookmarkFile),
	}
	if b, err := os.ReadFile(e.bookmark); err == nil {
		if v, perr := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); perr == nil {
			e.lastRecord = v
			e.baselined = true // resuming: emit everything after the bookmark
		}
	}
	return e
}

// Collect queries the Application channel and returns crash/hang events newer
// than the bookmark. The first poll only establishes the baseline (so freshly
// installed agents don't replay historical crashes).
func (e *winEvents) Collect() []model.Event {
	pathPtr, _ := windows.UTF16PtrFromString("Application")
	queryPtr, _ := windows.UTF16PtrFromString(eventQuery)

	hResults, _, callErr := procEvtQuery.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(queryPtr)),
		uintptr(evtQueryChannelPath|evtQueryForwardDirection|evtQueryTolerateQueryErrors),
	)
	if hResults == 0 {
		e.log.Warn("EvtQuery failed", "err", callErr.Error())
		return nil
	}
	defer procEvtClose.Call(hResults)

	var out []model.Event
	maxRecord := e.lastRecord
	total := 0

	for total < maxEventsPerPoll {
		var handles [eventsPerPoll]uintptr
		var returned uint32
		r, _, _ := procEvtNext.Call(
			hResults,
			uintptr(len(handles)),
			uintptr(unsafe.Pointer(&handles[0])),
			1000, // timeout ms
			0,
			uintptr(unsafe.Pointer(&returned)),
		)
		if r == 0 || returned == 0 {
			break // ERROR_NO_MORE_ITEMS or timeout
		}
		for i := uint32(0); i < returned; i++ {
			h := handles[i]
			ev, rec, ok := e.parseEvent(h)
			procEvtClose.Call(h)
			total++
			if rec > maxRecord {
				maxRecord = rec
			}
			if ok && e.baselined && rec > e.lastRecord {
				out = append(out, ev)
			}
		}
	}

	if maxRecord != e.lastRecord {
		e.lastRecord = maxRecord
		if err := os.WriteFile(e.bookmark, []byte(strconv.FormatUint(maxRecord, 10)), 0o640); err != nil {
			e.log.Debug("persist event bookmark failed", "err", err.Error())
		}
	}
	if !e.baselined {
		e.baselined = true // subsequent polls emit new events
		return nil
	}
	return out
}

// parseEvent renders one event to XML and extracts the allowlisted fields.
func (e *winEvents) parseEvent(h uintptr) (model.Event, uint64, bool) {
	xmlStr := renderEventXML(h)
	if xmlStr == "" {
		return model.Event{}, 0, false
	}
	var doc evtXML
	if err := xml.Unmarshal([]byte(xmlStr), &doc); err != nil {
		return model.Event{}, 0, false
	}

	rec := doc.System.EventRecordID
	provider := doc.System.Provider.Name
	id := doc.System.EventID.Value

	var kind string
	switch {
	case provider == "Application Error" && id == 1000:
		kind = "app_crash"
	case provider == "Application Hang" && id == 1002:
		kind = "app_hang"
	default:
		return model.Event{}, rec, false
	}

	ev := model.Event{
		TS:   parseEventTime(doc.System.TimeCreated.SystemTime),
		Kind: kind,
	}
	if len(doc.EventData.Data) > 0 {
		// Data[0] is the faulting application name (base name only). Reduce to a
		// base name defensively and normalize — never a path or the message.
		ev.ProcessName = normalizeProcName(filepath.Base(doc.EventData.Data[0]))
	}
	if kind == "app_crash" {
		if code := extractException(doc.EventData.Data); code != "" {
			ev.Detail = map[string]string{"exception": code}
		}
	}
	return ev, rec, true
}

// extractException returns the first Data value that looks like an exception code
// (e.g. 0xc0000005). Application Error places it at index 6, but we validate by
// shape rather than trusting the position.
func extractException(data []string) string {
	if len(data) > 6 && exceptionRe.MatchString(strings.TrimSpace(data[6])) {
		return strings.ToLower(strings.TrimSpace(data[6]))
	}
	for _, d := range data {
		d = strings.TrimSpace(d)
		if exceptionRe.MatchString(d) {
			return strings.ToLower(d)
		}
	}
	return ""
}

func parseEventTime(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}

// renderEventXML renders an event handle to its XML string via EvtRender.
func renderEventXML(h uintptr) string {
	var used, propCount uint32
	// First call sizes the buffer.
	procEvtRender.Call(0, h, evtRenderEventXml, 0, 0,
		uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&propCount)))
	if used == 0 {
		return ""
	}
	buf := make([]byte, used)
	r, _, _ := procEvtRender.Call(0, h, evtRenderEventXml, uintptr(used),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&propCount)))
	if r == 0 {
		return ""
	}
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), used/2)
	return windows.UTF16ToString(u16)
}
