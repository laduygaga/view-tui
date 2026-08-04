package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadEpub(t *testing.T) {
	if _, err := os.Stat("alice.epub"); os.IsNotExist(err) {
		t.Skip("alice.epub not found, skipping TestReadEpub")
	}

	chapters, meta, err := readEpub("alice.epub")
	if err != nil {
		t.Fatalf("Failed to read epub: %v", err)
	}

	if meta.Title == "" {
		t.Error("Expected non-empty title")
	}
	if meta.Author == "" {
		t.Error("Expected non-empty author")
	}

	if len(chapters) == 0 {
		t.Fatal("Expected at least one chapter")
	}

	t.Logf("Successfully read EPUB: %s by %s. Total chapters: %d", meta.Title, meta.Author, len(chapters))

	if len(chapters) > 1 {
		t.Logf("Chapter 1 Title: %s", chapters[1].Title)
		lines := strings.Split(chapters[1].Body, "\n")
		for i := 0; i < 10 && i < len(lines); i++ {
			t.Logf("Line %d: %q", i, lines[i])
		}
	}
}

func TestReadPdf(t *testing.T) {
	if _, err := os.Stat("dummy.pdf"); os.IsNotExist(err) {
		t.Skip("dummy.pdf not found, skipping TestReadPdf")
	}

	chapters, meta, err := readPdf("dummy.pdf")
	if err != nil {
		t.Fatalf("Failed to read pdf: %v", err)
	}

	if meta.Title != "dummy.pdf" {
		t.Errorf("Expected title to be dummy.pdf, got %s", meta.Title)
	}

	if len(chapters) == 0 {
		t.Fatal("Expected at least one page")
	}

	t.Logf("Successfully read PDF: %s. Total pages: %d", meta.Title, len(chapters))
}

func TestReadingPositionPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "view-tui-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFilePath := filepath.Join(tmpDir, "sample.epub")

	expectedChapter := 3
	expectedYOffset := 42
	err = saveBookPosition(testFilePath, expectedChapter, expectedYOffset)
	if err != nil {
		t.Fatalf("Failed to save book position: %v", err)
	}

	pos, found := getBookPosition(testFilePath)
	if !found {
		t.Fatalf("Expected position to be found for %s", testFilePath)
	}

	if pos.ChapterIndex != expectedChapter {
		t.Errorf("Expected ChapterIndex %d, got %d", expectedChapter, pos.ChapterIndex)
	}
	if pos.YOffset != expectedYOffset {
		t.Errorf("Expected YOffset %d, got %d", expectedYOffset, pos.YOffset)
	}
}
