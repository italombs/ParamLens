//go:build windows

// Package desktop shows the interface in a native window (WebView2, the Edge engine that ships
// with Windows 10/11) and attaches a console only when one is needed. The release .exe is built
// with -H windowsgui, so by default there is no console window: one item in the taskbar, and
// closing the window ends the program.
package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The window has to be created and pumped by the main OS thread.
func init() { runtime.LockOSThread() }

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	user32           = windows.NewLazySystemDLL("user32.dll")
	procAttach       = kernel32.NewProc("AttachConsole")
	procAlloc        = kernel32.NewProc("AllocConsole")
	procGetConsole   = kernel32.NewProc("GetConsoleWindow")
	procSystemMetric = user32.NewProc("GetSystemMetrics")
	procGetDpi       = user32.NewProc("GetDpiForWindow")
	procShowWindow   = user32.NewProc("ShowWindow")
	procDwmSetAttr   = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
)

const attachParent = ^uintptr(0) // ATTACH_PARENT_PROCESS (-1)

// WindowAvailable reports whether the WebView2 runtime is installed.
func WindowAvailable() bool {
	v, err := webviewloader.GetInstalledVersion()
	return err == nil && v != ""
}

// Console makes sure the process has a console for its messages: the one of the terminal that
// started it or, when started from Explorer, a new console window (closing it ends the program).
func Console() {
	if h, _, _ := procGetConsole.Call(); h != 0 {
		return // built without -H windowsgui (go run, development builds)
	}
	if r, _, _ := procAttach.Call(attachParent); r == 0 {
		procAlloc.Call()
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
}

func hasConsole() bool {
	h, _, _ := procGetConsole.Call()
	return h != 0
}

// Fatal reports an error before quitting: in the console if there is one, otherwise in a message box.
func Fatal(msg string) {
	if hasConsole() {
		fmt.Println("Error:", msg)
		fmt.Println("Press Enter to exit.")
		fmt.Scanln()
		return
	}
	t, _ := syscall.UTF16PtrFromString("ParamLens")
	m, _ := syscall.UTF16PtrFromString(msg)
	windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR)
}

// RunWindow opens the interface in a native window and returns when the window is closed.
func RunWindow(title, url string) error {
	sw, _, _ := procSystemMetric.Call(0) // SM_CXSCREEN
	sh, _, _ := procSystemMetric.Call(1) // SM_CYSCREEN
	w, h := uint(sw)*85/100, uint(sh)*85/100
	if w < 900 || h < 600 {
		w, h = 1280, 800
	}
	data := ""
	if d, err := os.UserCacheDir(); err == nil {
		data = filepath.Join(d, "ParamLens", "WebView2")
	}
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  data,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: title, Width: w, Height: h, Center: true,
			IconId: 1, // the APP icon of winres/winres.json
		},
	})
	if wv == nil {
		return fmt.Errorf("could not start WebView2")
	}
	defer wv.Destroy()
	hwnd := uintptr(wv.Window())
	darkTitleBar(hwnd)
	// small screens (in CSS pixels, after the Windows scaling) get the whole screen
	if dpi, _, _ := procGetDpi.Call(hwnd); dpi > 0 && sw*96/dpi < 1700 {
		procShowWindow.Call(hwnd, 3) // SW_MAXIMIZE
	}
	wv.Navigate(url)
	wv.Run()
	return nil
}

// darkTitleBar matches the title bar to the Windows app theme (the page follows it too).
func darkTitleBar(hwnd uintptr) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("AppsUseLightTheme"); err != nil || v != 0 {
		return
	}
	on := int32(1)
	procDwmSetAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&on)), 4) // DWMWA_USE_IMMERSIVE_DARK_MODE
}
