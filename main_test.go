package main

import (
	"strings"
	"testing"
)

func TestReadEpub(t *testing.T) {
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
