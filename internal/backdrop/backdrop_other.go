//go:build !windows

package backdrop

import (
	"fmt"
	"runtime"
)

// Show cannot cover a desktop this platform does not have. The command still
// compiles everywhere - the same position the library takes
// (docs/decisions/0007) - and answers honestly at run time instead.
func Show(colour Colour) error {
	return fmt.Errorf("mullion backdrop opens a Win32 window and runs on Windows only; this is %s", runtime.GOOS)
}
