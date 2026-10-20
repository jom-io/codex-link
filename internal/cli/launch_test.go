package cli

import (
	"errors"
	"testing"
	"time"
)

func TestStartLaunchAgentWaitsForAsynchronousBootout(t *testing.T) {
	calls := []string{}
	attempt := 0
	e := startLaunchAgent(func() bool { return attempt < 2 }, func(args ...string) error {
		attempt++
		calls = append(calls, args[0])
		if attempt < 3 {
			return errors.New("registration changing")
		}
		return nil
	}, "gui/501", "gui/501/own", "/own.plist", func(time.Duration) {})
	if e != nil || len(calls) != 3 || calls[0] != "kickstart" || calls[2] != "bootstrap" {
		t.Fatal(calls, e)
	}
}
