package archive

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestGunzipTo(t *testing.T) {
	// Create a gzipped file
	dir := t.TempDir()
	src := filepath.Join(dir, "test.gz")
	dest := filepath.Join(dir, "output.txt")

	original := []byte("hello world")

	// Write gzipped content
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write(original)
	gw.Close()
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Gunzip
	if err := GunzipTo(src, dest); err != nil {
		t.Fatalf("GunzipTo failed: %v", err)
	}

	// Verify
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("content = %q, want %q", got, original)
	}
}

func TestGunzipToInvalidGzip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "invalid.gz")
	dest := filepath.Join(dir, "output.txt")

	if err := os.WriteFile(src, []byte("not gzipped"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GunzipTo(src, dest); err == nil {
		t.Error("expected error for invalid gzip")
	}
}

func TestUnzipMemberTo(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.zip")
	dest := filepath.Join(dir, "output.txt")

	// Create a zip file
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("mihomo-linux-amd64")
	w.Write([]byte("binary content"))
	zw.Close()

	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Extract with tokens
	tokens := []string{"mihomo", "linux"}
	if err := UnzipMemberTo(src, tokens, dest); err != nil {
		t.Fatalf("UnzipMemberTo failed: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary content" {
		t.Errorf("content = %q", got)
	}
}

func TestUnzipMemberToNoMatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.zip")
	dest := filepath.Join(dir, "output.txt")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("other-file")
	w.Write([]byte("content"))
	zw.Close()

	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	tokens := []string{"mihomo"}
	if err := UnzipMemberTo(src, tokens, dest); err == nil {
		t.Error("expected error for no matching member")
	}
}

func TestUnzipAllTo(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.zip")
	destDir := t.TempDir()

	// Create a zip with multiple files
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w1, _ := zw.Create("subdir/file1.txt")
	w1.Write([]byte("content1"))
	w2, _ := zw.Create("subdir/file2.txt")
	w2.Write([]byte("content2"))
	zw.Close()

	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UnzipAllTo(src, destDir); err != nil {
		t.Fatalf("UnzipAllTo failed: %v", err)
	}

	// Verify files extracted (common top dir stripped)
	for _, f := range []string{"file1.txt", "file2.txt"} {
		path := filepath.Join(destDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("file %q not extracted", f)
		}
	}
}

func TestBaseContainsAll(t *testing.T) {
	tests := []struct {
		base   string
		tokens []string
		want   bool
	}{
		{"mihomo-linux-amd64.gz", []string{"mihomo", "linux"}, true},
		{"mihomo-linux-amd64.gz", []string{"windows"}, false},
		{"MIHOMO-LINUX-AMD64.GZ", []string{"mihomo", "linux"}, true}, // case-insensitive
		{"file.zip", []string{}, true},                               // empty tokens
	}

	for _, tt := range tests {
		got := baseContainsAll(tt.base, tt.tokens)
		if got != tt.want {
			t.Errorf("baseContainsAll(%q, %v) = %v, want %v", tt.base, tt.tokens, got, tt.want)
		}
	}
}

func TestCommonTopDir(t *testing.T) {
	tests := []struct {
		name  string
		files []string // file names in zip
		want  string   // expected common top dir
	}{
		{
			name:  "common subdir",
			files: []string{"subdir/a.txt", "subdir/b.txt"},
			want:  "subdir/",
		},
		{
			name:  "no common dir",
			files: []string{"a.txt", "b.txt"},
			want:  "",
		},
		{
			name:  "mixed",
			files: []string{"dir/a.txt", "other/b.txt"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create zip.File slice
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for _, f := range tt.files {
				w, _ := zw.Create(f)
				w.Write([]byte("x"))
			}
			zw.Close()

			// Read back
			r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}

			got := commonTopDir(r.File)
			if got != tt.want {
				t.Errorf("commonTopDir = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "output.txt")

	content := []byte("test content")
	reader := bytes.NewReader(content)

	if err := writeFile(dest, reader, 0o644); err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}

	// Verify file exists
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestWriteFileToNestedPath(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "subdir", "nested", "file.txt")

	content := []byte("nested content")
	if err := writeFile(dest, bytes.NewReader(content), 0o644); err != nil {
		t.Fatalf("writeFile to nested path failed: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch")
	}
}
