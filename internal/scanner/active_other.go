//go:build !linux && !darwin

package scanner

// claudeProcesses has no implementation on unsupported platforms; active
// session detection is simply disabled there.
func claudeProcesses() []claudeProc { return nil }
