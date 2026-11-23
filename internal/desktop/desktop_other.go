//go:build !windows

package desktop

import (
	"errors"
	"fmt"
)

// WindowAvailable is false outside Windows: the interface opens in the browser.
func WindowAvailable() bool { return false }

// Console is a no-op: the program already runs in a terminal.
func Console() {}

// Fatal prints the error.
func Fatal(msg string) { fmt.Println("Error:", msg) }

// RunWindow is not available outside Windows.
func RunWindow(title, url string) error {
	return errors.New("native window not available on this system")
}
