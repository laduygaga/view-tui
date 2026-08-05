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

func TestChienTranhTienTePDF(t *testing.T) {
	pdfPath := "/home/duy/Documents/nhasachmienphi-chien-tranh-tien-te.pdf"
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skip("PDF file not found")
	}

	chapters, meta, err := readPdf(pdfPath)
	if err != nil {
		t.Fatalf("Failed to read pdf: %v", err)
	}
	t.Logf("Read %d pages, title: %s", len(chapters), meta.Title)
	if len(chapters) > 1 {
		t.Logf("Page 2 body:\n%s", chapters[1].Body)
	}
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

func TestParseGoogleTranslateResponse(t *testing.T) {
	sampleJSON := `[[["Xin chào thế giới. ","Hello world. ",null,null,3,null,null,[[]],[]],["Đây là một bài kiểm tra.","This is a test.",null,null,3,null,null,[[]],[]]],null,"en"]`
	result, err := parseGoogleTranslateResponse([]byte(sampleJSON))
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	expected := "Xin chào thế giới. Đây là một bài kiểm tra."
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestWrapTextWithVietnamese(t *testing.T) {
	text := "Xin chào thế giới. Đây là một đoạn văn bản tiếng Việt với các từ có dấu."
	wrapped := wrapText(text, 30)
	if wrapped == "" {
		t.Error("Expected non-empty wrapped text")
	}
	lines := strings.Split(wrapped, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "  ") {
			l = l[2:]
		}
		if len([]rune(l)) > 26 {
			t.Errorf("Line exceeds target width: %q (rune count: %d)", l, len([]rune(l)))
		}
	}
}

func TestTranslateToVietnamese(t *testing.T) {
	res, err := translateToVietnamese("Hello world. How are you?")
	if err != nil {
		t.Fatalf("Translate error: %v", err)
	}
	if !strings.Contains(strings.ToLower(res), "chào") && !strings.Contains(strings.ToLower(res), "thế giới") {
		t.Errorf("Expected Vietnamese translation, got: %s", res)
	}
}
