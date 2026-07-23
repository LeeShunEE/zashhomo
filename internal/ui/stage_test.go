package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"}, {512, "512B"}, {1023, "1023B"},
		{1024, "1.0KB"}, {2048, "2.0KB"},
		{1 << 20, "1.0MB"}, {1536000, "1.5MB"},
		{1 << 30, "1.0GB"},
	}
	for _, c := range cases {
		if got := HumanBytes(c.in); got != c.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBarLockedKnownTotal(t *testing.T) {
	s := &Stage{label: "x", total: 100}
	s.cur = 50
	bar := s.barLocked()
	if !strings.Contains(bar, "50%") || !strings.Contains(bar, "[") || !strings.Contains(bar, ">") {
		t.Errorf("expected a 50%% partial bar, got %q", bar)
	}
	// Complete: no '>' marker.
	s.cur = 100
	bar = s.barLocked()
	if strings.Contains(bar, ">") {
		t.Errorf("complete bar should have no '>': %q", bar)
	}
}

func TestBarLockedUnknownTotal(t *testing.T) {
	s := &Stage{label: "x", total: 0}
	s.cur = 1 << 20
	bar := s.barLocked()
	if strings.Contains(bar, "[") || strings.Contains(bar, "%") {
		t.Errorf("unknown total bar should have no bar/percent: %q", bar)
	}
	if !strings.Contains(bar, "MB") {
		t.Errorf("unknown total bar should show byte counter: %q", bar)
	}
}

func TestStageDownloadNonTTY(t *testing.T) {
	payload := bytes.Repeat([]byte("zashhomo"), 1000) // 8000 bytes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	var out bytes.Buffer
	s := &Stage{label: "downloading", out: &out, tty: false}
	if err := s.Download(srv.URL, dest); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch: got %d bytes, want %d", len(got), len(payload))
	}
	// Non-TTY must not emit any carriage-return animation.
	if strings.Contains(out.String(), "\r") {
		t.Errorf("non-TTY download must not animate, got %q", out.String())
	}
}

func TestStageDownloadTTY(t *testing.T) {
	payload := []byte("hello world payload data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tty.bin")
	var out bytes.Buffer
	s := &Stage{label: "downloading", out: &out, tty: true}
	if err := s.Download(srv.URL, dest); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !strings.Contains(out.String(), "downloading ") {
		t.Errorf("expected animated progress output, got %q", out.String())
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch")
	}
}
