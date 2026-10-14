// Package winproc holds the one Windows process-creation primitive the agent
// shares between its two child spawners (the in-session foreground helper and
// the SYSTEM Appx removal worker): a STARTUPINFOEX whose
// PROC_THREAD_ATTRIBUTE_HANDLE_LIST names exactly the handles a child may
// inherit. It is empty on non-Windows builds.
package winproc
