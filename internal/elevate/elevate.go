package elevate

import "errors"

// ErrChildReported signals that the elevated child process already relayed its
// own error output to the console. Callers should treat this as a failure but
// stay silent — printing another line would just duplicate the child's message.
var ErrChildReported = errors.New("elevated child already reported its error")

// ElevatedLogFlag is a private command-line flag the Windows UAC relauncher
// appends when re-running zashhomo elevated. The child process redirects its
// stdout/stderr to the file path that follows the flag, and the (non-elevated)
// parent prints that file in the original console once the child exits. This
// lets elevated operations run without a separate, self-closing console window.
const ElevatedLogFlag = "--elevated-log"
