//go:build windows

package main

import "syscall"

// enableUTF8Console switches the console to UTF-8 so Turkish text is not shown
// through the legacy OEM code page.
func enableUTF8Console() {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	_, _, _ = k32.NewProc("SetConsoleOutputCP").Call(65001)
	_, _, _ = k32.NewProc("SetConsoleCP").Call(65001)
}
