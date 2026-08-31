//go:build windows

package host

import (
	"errors"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// registerRequiredDocumentCreatedScripts is the mandatory post-embed barrier
// for the first document. Browser serializes each dependency-ordered Add with
// its documented successful completion before the next Add can begin.
func (host *Host) registerRequiredDocumentCreatedScripts(browser *webview2.Browser, register func(...string) error) error {
	err := host.committedBrowserStepOrTearDown(func() error {
		if host.windowDestroyed || host.browser != browser {
			return errors.New("window destroyed during document-created script registration")
		}
		if err := register(host.js.bridge, host.js.diagnostics, host.js.drag, host.js.resize); err != nil {
			return err
		}
		if host.windowDestroyed || host.browser != browser {
			return errors.New("window destroyed during document-created script registration")
		}
		return nil
	})
	if err != nil {
		return errors.Join(errors.New("register required document-created scripts"), err)
	}
	return nil
}
