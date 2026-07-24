// Package ui provides terminal progress affordances for long-running CLI steps:
// a braille spinner and a download progress bar. When stderr is not a terminal
// (pipes, log files, the service running in the background) it degrades to
// plain line prints with no \r animation, so logs stay clean.
package ui

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Stage animates one step: a braille spinner while waiting/working, a progress
// bar while downloading, finalized with a one-line result. It is inert when
// stderr is not a terminal.
type Stage struct {
	label string
	out   io.Writer
	tty   bool

	mu      sync.Mutex
	phase   string // "spin" | "progress"
	frame   int
	cur     int64
	total   int64
	lastLen int
	stop    chan struct{}
	stopped chan struct{}
}

// NewStage creates a stage for a labelled step.
func NewStage(label string) *Stage {
	return &Stage{label: label, out: os.Stderr, tty: IsTerminal(os.Stderr)}
}

// Start begins the spinner animation (or prints the label when not a TTY).
func (s *Stage) Start() {
	if !s.tty {
		fmt.Fprintf(s.out, "• %s…\n", s.label)
		return
	}
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	s.mu.Lock()
	s.renderLocked()
	s.mu.Unlock()
	go s.animate()
}

func (s *Stage) animate() {
	defer close(s.stopped)
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			s.frame = (s.frame + 1) % len(brailleFrames)
			s.renderLocked()
			s.mu.Unlock()
		}
	}
}

// renderLocked draws the current line, padded to clear leftover chars. The
// caller must hold s.mu.
func (s *Stage) renderLocked() {
	var b strings.Builder
	b.WriteString(s.label)
	b.WriteByte(' ')
	if s.phase == "progress" {
		b.WriteString(s.barLocked())
	} else {
		b.WriteString(brailleFrames[s.frame])
	}
	line := b.String()
	if len(line) < s.lastLen {
		line += strings.Repeat(" ", s.lastLen-len(line))
	}
	s.lastLen = len(line)
	fmt.Fprintf(s.out, "\r%s", line)
}

func (s *Stage) barLocked() string {
	const width = 20
	if s.total > 0 {
		frac := float64(s.cur) / float64(s.total)
		if frac > 1 {
			frac = 1
		}
		filled := int(frac * float64(width))
		bar := strings.Repeat("=", filled)
		if filled < width {
			bar += ">"
		}
		bar += strings.Repeat(" ", width-filled)
		return fmt.Sprintf("[%s] %3d%% %s/%s", bar, int(frac*100), HumanBytes(s.cur), HumanBytes(s.total))
	}
	// Unknown length: a spinner plus the byte counter.
	return fmt.Sprintf("%s %s", brailleFrames[s.frame], HumanBytes(s.cur))
}

// Download fetches url into dest atomically (temp file + rename), animating a
// progress bar on the stage line. After it returns the spinner resumes so the
// caller can keep working (e.g. extracting) under the same animation.
func (s *Stage) Download(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "zashhomo")
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	track := s.tty
	if track {
		s.mu.Lock()
		s.phase = "progress"
		s.total = resp.ContentLength
		s.cur = 0
		s.renderLocked()
		s.mu.Unlock()
	}
	copyErr := s.copy(tmp, resp.Body, track)
	closeErr := tmp.Close()
	if track {
		s.resumeSpin()
	}
	if copyErr != nil {
		os.Remove(tmpName)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return closeErr
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// copy streams src to dst, updating s.cur when track is set.
func (s *Stage) copy(dst io.Writer, src io.Reader, track bool) error {
	buf := make([]byte, 32*1024)
	var n int64
	for {
		read, err := src.Read(buf)
		if read > 0 {
			if _, werr := dst.Write(buf[:read]); werr != nil {
				return werr
			}
			n += int64(read)
			if track {
				s.mu.Lock()
				s.cur = n
				s.mu.Unlock()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// resumeSpin returns the stage to the spinner phase after a download.
func (s *Stage) resumeSpin() {
	s.mu.Lock()
	s.phase = "spin"
	s.cur = 0
	s.total = 0
	s.frame = 0
	s.renderLocked()
	s.mu.Unlock()
}

// Done stops the animation and prints the final line. With a result of "" the
// stage is cleared without a final line.
func (s *Stage) Done(result string) {
	if !s.tty {
		if result != "" {
			fmt.Fprintf(s.out, "  → %s\n", result)
		}
		return
	}
	if s.stop != nil {
		close(s.stop)
		<-s.stopped
		s.stop = nil
	}
	if result == "" {
		// Clear the animation line.
		fmt.Fprintf(s.out, "\r%s\r", strings.Repeat(" ", s.lastLen))
		s.lastLen = 0
		return
	}
	line := s.label + " " + result
	if len(line) < s.lastLen {
		line += strings.Repeat(" ", s.lastLen-len(line))
	}
	fmt.Fprintf(s.out, "\r%s\n", line)
	s.lastLen = 0
}

// Run animates fn under a braille spinner labelled label, finalizing the line
// with success when fn returns nil and "failed" when it does not. It is the
// one-liner form of Start/Done for steps that only need to show they are alive,
// and it degrades to plain line prints when stderr is not a terminal.
//
// Steps that finish instantly are not a special case: Done always prints the
// final line, so a fast step simply renders its result without a visible frame.
func Run(label, success string, fn func() error) error {
	s := NewStage(label)
	s.Start()
	if err := fn(); err != nil {
		s.Done("failed")
		return err
	}
	s.Done(success)
	return nil
}

// RunValue is Run for a step that produces a value, with the final line's text
// derived from that value by result. On failure the value is returned as its
// zero value alongside the error.
func RunValue[T any](label string, fn func() (T, error), result func(T) string) (T, error) {
	s := NewStage(label)
	s.Start()
	v, err := fn()
	if err != nil {
		s.Done("failed")
		var zero T
		return zero, err
	}
	s.Done(result(v))
	return v, nil
}

// IsTerminal reports whether f is a terminal (a character device) without a
// third-party TTY library. Pipes and regular files are not terminals.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// HumanBytes formats a byte count compactly (e.g. 1.2MB).
func HumanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
