package localai

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractZipRejectsTraversalAndExtractsServer(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "llama.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("llama-server.exe")
	_, _ = entry.Write([]byte("server"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := extractZip(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	if !regularFile(filepath.Join(destination, "llama-server.exe")) {
		t.Fatal("server was not extracted")
	}

	unsafePath := filepath.Join(t.TempDir(), "unsafe.zip")
	unsafeFile, _ := os.Create(unsafePath)
	unsafeWriter := zip.NewWriter(unsafeFile)
	unsafeEntry, _ := unsafeWriter.Create("../escape.txt")
	_, _ = unsafeEntry.Write([]byte("no"))
	_ = unsafeWriter.Close()
	_ = unsafeFile.Close()
	if err := extractZip(unsafePath, t.TempDir()); err == nil {
		t.Fatal("unsafe archive path was accepted")
	}
}
