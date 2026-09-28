package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpandListsDirectoryImages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jpg"))
	writeFile(t, filepath.Join(dir, "b.png"))
	writeFile(t, filepath.Join(dir, "notes.txt"))
	if err := os.Mkdir(filepath.Join(dir, "epaper"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := expand([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.jpg"), filepath.Join(dir, "b.png")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExpandRejectsEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := expand([]string{dir}); err == nil {
		t.Fatal("expected an error for a directory with no images")
	}
}

func TestSingleDirOnlyForOneDirectoryArg(t *testing.T) {
	dir := t.TempDir()
	if _, ok := singleDir([]string{dir}); !ok {
		t.Fatal("a single directory argument should match")
	}
	if _, ok := singleDir([]string{dir, dir}); ok {
		t.Fatal("two arguments should not match")
	}
	file := filepath.Join(dir, "a.jpg")
	writeFile(t, file)
	if _, ok := singleDir([]string{file}); ok {
		t.Fatal("a file argument should not match")
	}
}
