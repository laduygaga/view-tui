package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dslipak/pdf"
	"github.com/taylorskalyo/goreader/epub"
	"golang.org/x/net/html"
)

type Chapter struct {
	Title string
	Body  string
}

type BookMetadata struct {
	Title  string
	Author string
	Type   string // "epub" or "pdf"
}

type appMode int

const (
	modeFilePicker appMode = iota
	modeReader
)

type fileEntry struct {
	name     string
	fullPath string
	size     int64
}

type ReadingPosition struct {
	ChapterIndex int `json:"chapter_index"`
	YOffset      int `json:"y_offset"`
}

func getPositionsFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	appDir := filepath.Join(configDir, "view-tui")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(appDir, "positions.json"), nil
}

func loadPositions() (map[string]ReadingPosition, error) {
	path, err := getPositionsFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]ReadingPosition), nil
		}
		return nil, err
	}
	var positions map[string]ReadingPosition
	if err := json.Unmarshal(data, &positions); err != nil {
		return make(map[string]ReadingPosition), nil
	}
	if positions == nil {
		positions = make(map[string]ReadingPosition)
	}
	return positions, nil
}

func saveBookPosition(filePath string, chapterIndex int, yOffset int) error {
	absPath, err := filepath.Abs(filePath)
	if err == nil {
		filePath = absPath
	}
	positions, err := loadPositions()
	if err != nil {
		positions = make(map[string]ReadingPosition)
	}
	positions[filePath] = ReadingPosition{
		ChapterIndex: chapterIndex,
		YOffset:      yOffset,
	}
	posPath, err := getPositionsFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(positions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(posPath, data, 0644)
}

func getBookPosition(filePath string) (ReadingPosition, bool) {
	absPath, err := filepath.Abs(filePath)
	if err == nil {
		filePath = absPath
	}
	positions, err := loadPositions()
	if err != nil {
		return ReadingPosition{}, false
	}
	pos, found := positions[filePath]
	return pos, found
}

var (
	styleTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Bold(true).
			Padding(0, 1)

	styleAuthor = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A3A3A3")).
			Italic(true)

	styleHeader = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("#3C3C3C")).
			PaddingBottom(1)

	styleFooter = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(lipgloss.Color("#3C3C3C")).
			PaddingTop(1).
			Foreground(lipgloss.Color("#757575"))

	styleProgress = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#353535")).
			Padding(0, 1)

	styleFilePickerHeader = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FAFAFA")).
				Background(lipgloss.Color("#F25D94")).
				Bold(true).
				Padding(0, 2)

	styleFilePickerSelected = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FAFAFA")).
				Background(lipgloss.Color("#F25D94")).
				Bold(true)

	styleHelpKey = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2E5BFF")).
			Bold(true)

	styleHelpDesc = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4A4A4A"))

	styleError = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true).
			Padding(0, 1)
)

type model struct {
	mode                appMode
	files               []fileEntry
	filteredFiles       []fileEntry
	fileIndex           int
	errorMessage        string
	loading             bool
	cliMode             bool // true if path was passed as argument

	currentFilePath     string
	metadata            BookMetadata
	chapters            []Chapter
	currentChapterIndex int
	viewport            viewport.Model
	width               int
	height              int

	searching           bool
	searchQuery         string
	searchMatches       []int
	searchActiveIndex   int

	filePickerInput     textinput.Model
	readerSearchInput   textinput.Model

	showHelp            bool
	lastKey             string

	showingTranslation  bool
	translating         bool
	translatedChapters  map[int]string
	translationError    string

	isSpeaking          bool
	ttsCancel           context.CancelFunc
	ttsError            string
	ttsSpeed            float64
	ttsSessionID        int64
}

func (m *model) saveCurrentPosition() {
	if m.mode == modeReader && m.currentFilePath != "" {
		_ = saveBookPosition(m.currentFilePath, m.currentChapterIndex, m.viewport.YOffset)
	}
}

func htmlToTextAndTitle(r io.Reader) (string, string) {
	var bodyBuf bytes.Buffer
	var title string

	z := html.NewTokenizer(r)
	inHead := false
	inTitle := false
	inStyle := false
	inScript := false

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return cleanText(bodyBuf.String()), title
			}
			return cleanText(bodyBuf.String()), title

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, _ := z.TagName()
			name := string(tn)
			if name == "head" {
				inHead = true
			} else if name == "title" && inHead {
				inTitle = true
			} else if name == "style" {
				inStyle = true
			} else if name == "script" {
				inScript = true
			} else if name == "br" || name == "p" || name == "div" || name == "h1" || name == "h2" || name == "h3" || name == "h4" || name == "h5" || name == "h6" || name == "li" || name == "tr" {
				bodyBuf.WriteString("\n")
			}

		case html.EndTagToken:
			tn, _ := z.TagName()
			name := string(tn)
			if name == "head" {
				inHead = false
			} else if name == "title" {
				inTitle = false
			} else if name == "style" {
				inStyle = false
			} else if name == "script" {
				inScript = false
			} else if name == "p" || name == "div" || name == "h1" || name == "h2" || name == "h3" || name == "h4" || name == "h5" || name == "h6" || name == "li" || name == "tr" {
				bodyBuf.WriteString("\n")
			}

		case html.TextToken:
			text := string(z.Text())
			if inTitle {
				title = strings.TrimSpace(text)
			} else if !inStyle && !inScript {
				bodyBuf.WriteString(text)
			}
		}
	}
}

func cleanText(s string) string {
	lines := strings.Split(s, "\n")
	var cleaned []string
	consecutiveBlanks := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = html.UnescapeString(trimmed)

		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				cleaned = append(cleaned, "")
			}
		} else {
			consecutiveBlanks = 0
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

type translatedMsg struct {
	chapterIndex int
	translated   string
	err          error
}

func translateChapterCmd(chapterIndex int, body string) tea.Cmd {
	return func() tea.Msg {
		translated, err := translateToVietnamese(body)
		return translatedMsg{
			chapterIndex: chapterIndex,
			translated:   translated,
			err:          err,
		}
	}
}

func translateToVietnamese(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}

	const maxChunkSize = 2500
	paragraphs := strings.Split(text, "\n")

	var chunks []string
	var currentChunk strings.Builder

	for _, p := range paragraphs {
		if currentChunk.Len() > 0 && currentChunk.Len()+len(p)+1 > maxChunkSize {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()
		}
		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n")
		}
		currentChunk.WriteString(p)
	}
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	var translatedChunks []string
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			translatedChunks = append(translatedChunks, chunk)
			continue
		}

		formData := url.Values{}
		formData.Set("q", chunk)

		reqURL := "https://translate.googleapis.com/translate_a/single?client=gtx&sl=auto&tl=vi&dt=t"
		req, err := http.NewRequest("POST", reqURL, strings.NewReader(formData.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d from translate API", resp.StatusCode)
		}

		translatedChunk, err := parseGoogleTranslateResponse(bodyBytes)
		if err != nil {
			return "", err
		}
		translatedChunks = append(translatedChunks, translatedChunk)
	}

	return strings.Join(translatedChunks, "\n"), nil
}

func parseGoogleTranslateResponse(data []byte) (string, error) {
	var raw []interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}

	if len(raw) == 0 {
		return "", fmt.Errorf("empty translation response")
	}

	segments, ok := raw[0].([]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response structure")
	}

	var sb strings.Builder
	for _, seg := range segments {
		segList, ok := seg.([]interface{})
		if !ok || len(segList) == 0 {
			continue
		}
		translatedText, ok := segList[0].(string)
		if ok {
			sb.WriteString(translatedText)
		}
	}

	return sb.String(), nil
}

type ttsFinishedMsg struct {
	sessionID int64
	err       error
}

func (m *model) stopTTS() {
	m.ttsSessionID++
	if m.ttsCancel != nil {
		m.ttsCancel()
		m.ttsCancel = nil
	}
	m.isSpeaking = false
}

func (m *model) getTTSSpeed() float64 {
	if m.ttsSpeed <= 0.1 {
		return 1.35
	}
	return m.ttsSpeed
}

func (m *model) startTTSCmd() tea.Cmd {
	m.stopTTS()

	text := m.getChapterBody()
	if strings.TrimSpace(text) == "" {
		m.ttsError = "No text to read"
		return nil
	}

	sessionID := m.ttsSessionID

	ctx, cancel := context.WithCancel(context.Background())
	m.ttsCancel = cancel
	m.isSpeaking = true
	m.ttsError = ""

	speed := m.getTTSSpeed()

	return func() tea.Msg {
		err := speakText(ctx, text, speed)
		return ttsFinishedMsg{
			sessionID: sessionID,
			err:       err,
		}
	}
}

func detectLanguage(text string) string {
	vietnameseUniqueChars := "đđơớờởỡợưứừửữựăắằẳẵặảẳẩẻểỉỏổởủửỷãẵẫẽễĩõỗỡũữỹạặậẹệịọộợụựỵốồổỗộếềểễệấầẩẫậ"
	count := 0
	for _, r := range strings.ToLower(text) {
		if strings.ContainsRune(vietnameseUniqueChars, r) {
			count++
		}
	}
	if count >= 3 {
		return "vi"
	}
	return "en"
}

func speakText(ctx context.Context, text string, speed float64) error {
	docLang := detectLanguage(text)

	chunks := splitTextForTTS(text, 120)
	if len(chunks) == 0 {
		return nil
	}

	for i, chunk := range chunks {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}

		if i > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(150 * time.Millisecond):
			}
		}

		chunkLang := docLang
		if docLang == "en" {
			if detectLanguage(chunk) == "vi" {
				chunkLang = "vi"
			}
		}

		err := speakChunk(ctx, chunk, chunkLang, speed)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func splitTextForTTS(text string, maxLen int) []string {
	paragraphs := strings.Split(text, "\n")
	var chunks []string

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		runes := []rune(p)
		if len(runes) <= maxLen {
			chunks = append(chunks, p)
			continue
		}

		var current []rune
		lastBreak := -1

		for _, r := range runes {
			current = append(current, r)

			if r == '.' || r == '!' || r == '?' || r == ';' || r == ',' || r == ' ' {
				lastBreak = len(current)
			}

			if len(current) >= maxLen {
				if lastBreak > 0 {
					chunkStr := strings.TrimSpace(string(current[:lastBreak]))
					if chunkStr != "" {
						chunks = append(chunks, chunkStr)
					}
					current = current[lastBreak:]
					lastBreak = -1
				} else {
					chunkStr := strings.TrimSpace(string(current))
					if chunkStr != "" {
						chunks = append(chunks, chunkStr)
					}
					current = nil
					lastBreak = -1
				}
			}
		}

		if len(current) > 0 {
			chunkStr := strings.TrimSpace(string(current))
			if chunkStr != "" {
				chunks = append(chunks, chunkStr)
			}
		}
	}

	return chunks
}

func splitChunkIfNeeded(chunk string, maxRunes int) []string {
	runes := []rune(chunk)
	if len(runes) <= maxRunes {
		return []string{chunk}
	}

	var parts []string
	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			parts = append(parts, string(runes))
			break
		}

		cut := maxRunes
		for i := maxRunes; i > 0; i-- {
			r := runes[i-1]
			if r == ' ' || r == '.' || r == ',' || r == '!' || r == '?' || r == ';' {
				cut = i
				break
			}
		}

		part := strings.TrimSpace(string(runes[:cut]))
		if part != "" {
			parts = append(parts, part)
		}
		runes = runes[cut:]
	}
	return parts
}

func speakChunk(ctx context.Context, chunk string, lang string, speed float64) error {
	if speed <= 0.1 {
		speed = 1.35
	}

	subChunks := splitChunkIfNeeded(chunk, 140)
	if len(subChunks) > 1 {
		for i, sc := range subChunks {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			if i > 0 {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(100 * time.Millisecond):
				}
			}
			err := speakChunk(ctx, sc, lang, speed)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
		}
		return nil
	}

	ttsURL := fmt.Sprintf("https://translate.google.com/translate_tts?ie=UTF-8&tl=%s&client=tw-ob&q=%s", lang, url.QueryEscape(chunk))

	var mp3Downloaded bool
	var tmpPath string

	tmpFile, err := os.CreateTemp("", "tts-*.mp3")
	if err == nil {
		tmpPath = tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(tmpPath)

		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil {
				return nil
			}

			req, err := http.NewRequestWithContext(ctx, "GET", ttsURL, nil)
			if err != nil {
				break
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					out, err := os.Create(tmpPath)
					if err == nil {
						_, err = io.Copy(out, resp.Body)
						out.Close()
						resp.Body.Close()

						if err == nil {
							mp3Downloaded = true
							break
						}
					} else {
						resp.Body.Close()
					}
				} else {
					resp.Body.Close()
				}
			}

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Duration(200*(attempt+1)) * time.Millisecond):
			}
		}
	}

	if ctx.Err() != nil {
		return nil
	}

	if mp3Downloaded {
		played := playAudioFile(ctx, tmpPath, speed)
		if ctx.Err() != nil {
			return nil
		}
		if played {
			return nil
		}
	}

	return speakOffline(ctx, chunk, lang, speed)
}

func playAudioFile(ctx context.Context, filePath string, speed float64) bool {
	players := []string{"mpv", "ffplay", "afplay", "paplay", "aplay"}
	for _, p := range players {
		path, err := exec.LookPath(p)
		if err != nil {
			continue
		}

		var cmd *exec.Cmd
		if strings.HasSuffix(path, "mpv") {
			cmd = exec.CommandContext(ctx, path, "--no-video", "--no-terminal", fmt.Sprintf("--speed=%.2f", speed), filePath)
		} else if strings.HasSuffix(path, "ffplay") {
			cmd = exec.CommandContext(ctx, path, "-nodisp", "-autoexit", "-loglevel", "quiet", "-af", fmt.Sprintf("atempo=%.2f", speed), filePath)
		} else {
			cmd = exec.CommandContext(ctx, path, filePath)
		}

		err = cmd.Run()
		if ctx.Err() != nil {
			return true
		}
		if err == nil {
			return true
		}
	}
	return false
}

func speakOffline(ctx context.Context, chunk string, lang string, speed float64) error {
	wpm := int(175.0 * speed)
	switch runtime.GOOS {
	case "darwin":
		if path, err := exec.LookPath("say"); err == nil {
			cmd := exec.CommandContext(ctx, path, "-r", strconv.Itoa(wpm), chunk)
			err := cmd.Run()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	case "linux":
		if path, err := exec.LookPath("espeak-ng"); err == nil {
			cmd := exec.CommandContext(ctx, path, "-s", strconv.Itoa(wpm), "-v", lang, chunk)
			err := cmd.Run()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if path, err := exec.LookPath("espeak"); err == nil {
			cmd := exec.CommandContext(ctx, path, "-s", strconv.Itoa(wpm), "-v", lang, chunk)
			err := cmd.Run()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if path, err := exec.LookPath("spd-say"); err == nil {
			rate := int((speed - 1.0) * 50)
			cmd := exec.CommandContext(ctx, path, "-r", strconv.Itoa(rate), "-l", lang, "-w", chunk)
			err := cmd.Run()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	case "windows":
		if path, err := exec.LookPath("powershell"); err == nil {
			escaped := strings.ReplaceAll(chunk, "'", "''")
			escaped = strings.ReplaceAll(escaped, `"`, `""`)
			rate := int((speed - 1.0) * 5)
			if rate < -10 {
				rate = -10
			}
			if rate > 10 {
				rate = 10
			}
			psScript := fmt.Sprintf(`Add-Type -AssemblyName System.Speech; $s = New-Object System.Speech.Synthesis.SpeechSynthesizer; $s.Rate = %d; $s.Speak('%s')`, rate, escaped)
			cmd := exec.CommandContext(ctx, path, "-NoProfile", "-NonInteractive", "-Command", psScript)
			err := cmd.Run()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}

	return fmt.Errorf("no TTS engine found (install mpv, ffplay, or espeak-ng)")
}

func readEpub(filePath string) ([]Chapter, BookMetadata, error) {
	rc, err := epub.OpenReader(filePath)
	if err != nil {
		return nil, BookMetadata{}, err
	}
	defer rc.Close()

	if len(rc.Rootfiles) == 0 {
		return nil, BookMetadata{}, fmt.Errorf("no rootfiles found in epub")
	}

	book := rc.Rootfiles[0]
	meta := BookMetadata{
		Title:  book.Title,
		Author: book.Creator,
		Type:   "epub",
	}

	if meta.Title == "" {
		meta.Title = filepath.Base(filePath)
	}
	if meta.Author == "" {
		meta.Author = "Unknown Author"
	}

	var chapters []Chapter
	for i, item := range book.Spine.Itemrefs {
		r, err := item.Open()
		if err != nil {
			continue
		}

		body, title := htmlToTextAndTitle(r)
		r.Close()

		if body == "" {
			continue
		}

		if title == "" {
			title = fmt.Sprintf("Chapter %d", i+1)
		}

		chapters = append(chapters, Chapter{
			Title: title,
			Body:  body,
		})
	}

	if len(chapters) == 0 {
		return nil, meta, fmt.Errorf("no readable chapters found in epub")
	}

	return chapters, meta, nil
}

func readPdf(filePath string) ([]Chapter, BookMetadata, error) {
	r, err := pdf.Open(filePath)
	if err != nil {
		return nil, BookMetadata{}, err
	}

	meta := BookMetadata{
		Title:  filepath.Base(filePath),
		Author: "Unknown Author",
		Type:   "pdf",
	}

	numPages := r.NumPage()
	if numPages == 0 {
		return nil, meta, fmt.Errorf("no pages found in pdf")
	}

	sampleLimit := 10
	if sampleLimit > numPages {
		sampleLimit = numPages
	}

	hasAnyFontsOrText := false
	for i := 1; i <= sampleLimit; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		if len(p.Fonts()) > 0 {
			hasAnyFontsOrText = true
			break
		}
		rows, err := p.GetTextByRow()
		if err == nil && len(rows) > 0 {
			hasAnyFontsOrText = true
			break
		}
	}

	if !hasAnyFontsOrText {
		return nil, meta, fmt.Errorf("no readable text found in pdf (this file appears to be a scanned/image-only PDF without an embedded text layer)")
	}

	pageChapters := make([]Chapter, numPages)

	numWorkers := runtime.NumCPU() * 2
	if numWorkers < 4 {
		numWorkers = 4
	}
	if numWorkers > numPages {
		numWorkers = numPages
	}

	pageChan := make(chan int, numPages)
	for i := 1; i <= numPages; i++ {
		pageChan <- i
	}
	close(pageChan)

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pageNum := range pageChan {
				func() {
					defer func() {
						_ = recover()
					}()

					p := r.Page(pageNum)
					if p.V.IsNull() {
						return
					}

					rows, err := p.GetTextByRow()
					var body string
					if err == nil {
						var pageBuf strings.Builder
						for _, row := range rows {
							for _, word := range row.Content {
								pageBuf.WriteString(word.S)
							}
							pageBuf.WriteString("\n")
						}
						body = strings.TrimSpace(pageBuf.String())
					}

					if body == "" {
						var fallbackBuf strings.Builder
						for _, text := range p.Content().Text {
							fallbackBuf.WriteString(text.S)
						}
						body = strings.TrimSpace(fallbackBuf.String())
					}

					if body == "" {
						return
					}

					pageChapters[pageNum-1] = Chapter{
						Title: fmt.Sprintf("Page %d", pageNum),
						Body:  body,
					}
				}()
			}
		}()
	}

	wg.Wait()

	var chapters []Chapter
	for _, ch := range pageChapters {
		if ch.Body != "" {
			chapters = append(chapters, ch)
		}
	}

	if len(chapters) == 0 {
		pt, err := r.GetPlainText()
		if err == nil {
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(pt)
			fullText := strings.TrimSpace(buf.String())
			if fullText != "" {
				chapters = append(chapters, Chapter{
					Title: "Document",
					Body:  fullText,
				})
			}
		}
	}

	if len(chapters) == 0 {
		return nil, meta, fmt.Errorf("no readable text found in pdf (this file appears to be a scanned/image-only PDF without an embedded text layer)")
	}

	return chapters, meta, nil
}

type loadedFilesMsg []fileEntry
type errMsg error

func loadFiles() tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return errMsg(err)
		}

		excludes := []string{
			".git", "__pycache__", "node_modules", ".local", ".cache",
			".cargo", ".npm", ".nvm", ".pyenv", ".venv", "venv",
			".rbenv", "rbenv", ".rustup", ".vscode", ".wine", ".wine32", ".wine64",
		}

		args := []string{".", "--base-directory", home, "-t", "f", "-H", "-e", "pdf", "-e", "epub"}
		for _, ex := range excludes {
			args = append(args, "-E", ex)
		}
		args = append(args, "--color", "never")

		cmd := exec.Command("fd", args...)
		output, err := cmd.Output()
		if err != nil {
			// fd returns 1 if no files are found in some versions, but we should treat it as empty list if exit code is 1 and no other error
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return loadedFilesMsg([]fileEntry{})
			}
			return errMsg(fmt.Errorf("fd error: %v", err))
		}

		lines := strings.Split(string(output), "\n")
		var files []fileEntry
		for _, line := range lines {
			path := strings.TrimSpace(line)
			if path == "" {
				continue
			}

			fullPath := path
			if !filepath.IsAbs(path) {
				fullPath = filepath.Join(home, path)
			}

			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}

			files = append(files, fileEntry{
				name:     path,
				fullPath: fullPath,
				size:     info.Size(),
			})
		}
		return loadedFilesMsg(files)
	}
}

func (m *model) filterFiles() {
	query := m.filePickerInput.Value()
	if query == "" {
		m.filteredFiles = m.files
	} else {
		m.filteredFiles = nil
		lowerQuery := strings.ToLower(query)
		for _, f := range m.files {
			if strings.Contains(strings.ToLower(f.name), lowerQuery) || strings.Contains(strings.ToLower(f.fullPath), lowerQuery) {
				m.filteredFiles = append(m.filteredFiles, f)
			}
		}
	}

	if m.fileIndex >= len(m.filteredFiles) {
		m.fileIndex = 0
	}
}

func (m *model) loadBook(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err == nil {
		filePath = absPath
	}

	var chapters []Chapter
	var meta BookMetadata

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".epub" {
		chapters, meta, err = readEpub(filePath)
	} else if ext == ".pdf" {
		chapters, meta, err = readPdf(filePath)
	} else {
		return fmt.Errorf("unsupported file extension: %s", ext)
	}

	if err != nil {
		return err
	}

	m.currentFilePath = filePath
	m.chapters = chapters
	m.metadata = meta

	pos, found := getBookPosition(filePath)
	if found && pos.ChapterIndex >= 0 && pos.ChapterIndex < len(chapters) {
		m.currentChapterIndex = pos.ChapterIndex
	} else {
		m.currentChapterIndex = 0
	}

	m.mode = modeReader
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchActiveIndex = -1
	m.lastKey = ""
	m.showHelp = false
	m.showingTranslation = false
	m.translating = false
	m.translatedChapters = make(map[int]string)
	m.translationError = ""
	m.stopTTS()
	m.ttsError = ""

	headerHeight := 4
	footerHeight := 4
	verticalMarginHeight := headerHeight + footerHeight

	vpWidth := m.width
	if vpWidth == 0 {
		vpWidth = 80
	}
	vpHeight := m.height - verticalMarginHeight
	if vpHeight < 5 {
		vpHeight = 20
	}

	m.viewport = viewport.New(vpWidth, vpHeight)
	m.viewport.SetContent(m.getProcessedChapterContent())
	if found && pos.ChapterIndex == m.currentChapterIndex {
		m.viewport.SetYOffset(pos.YOffset)
	} else {
		m.viewport.GotoTop()
	}

	return nil
}

func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	targetWidth := width - 4
	if targetWidth < 10 {
		targetWidth = 10
	}

	paragraphs := strings.Split(text, "\n")
	var wrappedParagraphs []string

	for _, p := range paragraphs {
		if p == "" {
			wrappedParagraphs = append(wrappedParagraphs, "")
			continue
		}

		words := strings.Fields(p)
		if len(words) == 0 {
			wrappedParagraphs = append(wrappedParagraphs, "")
			continue
		}

		var line strings.Builder
		line.WriteString("  ")
		line.WriteString(words[0])
		currentRuneCount := utf8.RuneCountInString(words[0])

		for _, word := range words[1:] {
			wordRunes := utf8.RuneCountInString(word)
			if currentRuneCount+1+wordRunes > targetWidth {
				wrappedParagraphs = append(wrappedParagraphs, line.String())
				line.Reset()
				line.WriteString("  ")
				line.WriteString(word)
				currentRuneCount = wordRunes
			} else {
				line.WriteString(" ")
				line.WriteString(word)
				currentRuneCount += 1 + wordRunes
			}
		}
		if line.Len() > 2 {
			wrappedParagraphs = append(wrappedParagraphs, line.String())
		}
	}

	return strings.Join(wrappedParagraphs, "\n")
}

func (m *model) getChapterBody() string {
	if len(m.chapters) == 0 {
		return ""
	}
	if m.showingTranslation {
		if translated, ok := m.translatedChapters[m.currentChapterIndex]; ok {
			return translated
		}
	}
	return m.chapters[m.currentChapterIndex].Body
}

func (m *model) getProcessedChapterContent() string {
	if len(m.chapters) == 0 {
		return ""
	}
	body := m.getChapterBody()
	wrapped := wrapText(body, m.width)
	if m.searchQuery != "" {
		return highlightText(wrapped, m.searchQuery, m.searchActiveIndex, m.searchMatches)
	}
	return wrapped
}

func highlightText(text, query string, activeMatchIndex int, matches []int) string {
	if query == "" {
		return text
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)

	var buf strings.Builder
	idx := 0
	matchCount := 0

	styleNormalMatch := lipgloss.NewStyle().Background(lipgloss.Color("11")).Foreground(lipgloss.Color("0"))
	styleActiveMatch := lipgloss.NewStyle().Background(lipgloss.Color("9")).Foreground(lipgloss.Color("15")).Bold(true)

	for {
		pos := strings.Index(lowerText[idx:], lowerQuery)
		if pos == -1 {
			buf.WriteString(text[idx:])
			break
		}

		absolutePos := idx + pos
		buf.WriteString(text[idx:absolutePos])

		matchedText := text[absolutePos : absolutePos+len(query)]
		if matchCount == activeMatchIndex {
			buf.WriteString(styleActiveMatch.Render(matchedText))
		} else {
			buf.WriteString(styleNormalMatch.Render(matchedText))
		}

		matchCount++
		idx = absolutePos + len(query)
	}

	return buf.String()
}

func (m *model) updateSearchMatches() {
	if m.searchQuery == "" {
		m.searchMatches = nil
		m.searchActiveIndex = -1
		return
	}

	body := m.getChapterBody()
	wrapped := wrapText(body, m.width)
	lowerBody := strings.ToLower(wrapped)
	lowerQuery := strings.ToLower(m.searchQuery)

	var matches []int
	idx := 0
	for {
		pos := strings.Index(lowerBody[idx:], lowerQuery)
		if pos == -1 {
			break
		}
		absolutePos := idx + pos
		matches = append(matches, absolutePos)
		idx = absolutePos + len(lowerQuery)
	}

	m.searchMatches = matches
	if len(matches) > 0 {
		m.searchActiveIndex = 0
		m.scrollToMatch()
	} else {
		m.searchActiveIndex = -1
	}
}

func (m *model) scrollToMatch() {
	if m.searchActiveIndex < 0 || m.searchActiveIndex >= len(m.searchMatches) {
		return
	}

	body := m.getChapterBody()
	wrapped := wrapText(body, m.width)
	matchPos := m.searchMatches[m.searchActiveIndex]

	linesCount := 0
	for i := 0; i < matchPos && i < len(wrapped); i++ {
		if wrapped[i] == '\n' {
			linesCount++
		}
	}

	targetY := linesCount - m.viewport.Height/2
	if targetY < 0 {
		targetY = 0
	}
	m.viewport.SetYOffset(targetY)
}

func (m model) Init() tea.Cmd {
	if m.mode == modeFilePicker && len(m.files) == 0 {
		return loadFiles()
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 4
		footerHeight := 4
		verticalMarginHeight := headerHeight + footerHeight

		if m.mode == modeReader {
			prevOffset := m.viewport.YOffset
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
			m.viewport.SetContent(m.getProcessedChapterContent())
			m.viewport.SetYOffset(prevOffset)
		}
		return m, nil

	case loadedFilesMsg:
		m.files = msg
		m.loading = false
		m.filterFiles()
		return m, nil

	case errMsg:
		m.errorMessage = msg.Error()
		m.loading = false
		return m, nil

	case translatedMsg:
		m.translating = false
		if msg.err != nil {
			m.translationError = fmt.Sprintf("Translation error: %v", msg.err)
			m.showingTranslation = false
		} else {
			if m.translatedChapters == nil {
				m.translatedChapters = make(map[int]string)
			}
			m.translatedChapters[msg.chapterIndex] = msg.translated
			if msg.chapterIndex == m.currentChapterIndex {
				m.showingTranslation = true
				m.translationError = ""
				m.viewport.SetContent(m.getProcessedChapterContent())
			}
		}
		return m, nil

	case ttsFinishedMsg:
		if msg.sessionID != m.ttsSessionID {
			return m, nil
		}
		m.isSpeaking = false
		m.ttsCancel = nil
		if msg.err != nil {
			m.ttsError = fmt.Sprintf("TTS error: %v", msg.err)
		} else {
			m.ttsError = ""
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeFilePicker:
			return m.updateFilePicker(msg)
		case modeReader:
			return m.updateReader(msg)
		}
	}
	return m, nil
}

func (m model) updateFilePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if len(m.filteredFiles) > 0 {
			selected := m.filteredFiles[m.fileIndex]
			filePath := selected.fullPath
			err := m.loadBook(filePath)
			if err != nil {
				m.errorMessage = err.Error()
			} else {
				m.errorMessage = ""
			}
		}
		return m, nil

	case tea.KeyEsc:
		if m.filePickerInput.Value() != "" {
			m.filePickerInput.SetValue("")
			m.filterFiles()
		} else {
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyCtrlJ, tea.KeyDown, tea.KeyCtrlN:
		if len(m.filteredFiles) > 0 {
			m.fileIndex = (m.fileIndex + 1) % len(m.filteredFiles)
		}
		return m, nil

	case tea.KeyCtrlK, tea.KeyUp, tea.KeyCtrlP:
		if len(m.filteredFiles) > 0 {
			m.fileIndex = (m.fileIndex - 1 + len(m.filteredFiles)) % len(m.filteredFiles)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.filePickerInput, cmd = m.filePickerInput.Update(msg)
	m.filterFiles()
	return m, cmd
}

func (m model) updateReader(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		return m.updateSearch(msg)
	}

	keyStr := msg.String()
	if msg.Type == tea.KeyRunes && strings.HasPrefix(keyStr, "/") {
		m.searching = true
		m.readerSearchInput.SetValue(keyStr[1:])
		m.readerSearchInput.Focus()
		m.searchQuery = m.readerSearchInput.Value()
		m.updateSearchMatches()
		m.viewport.SetContent(m.getProcessedChapterContent())
		return m, nil
	}

	if m.lastKey == "g" && keyStr == "g" {
		m.viewport.GotoTop()
		m.lastKey = ""
		m.saveCurrentPosition()
		return m, nil
	}
	if keyStr != "g" {
		m.lastKey = ""
	}

	switch keyStr {
	case "ctrl+c":
		m.stopTTS()
		m.saveCurrentPosition()
		return m, tea.Quit

	case "q":
		m.stopTTS()
		m.saveCurrentPosition()
		if m.showHelp {
			m.showHelp = false
		} else {
			if m.cliMode {
				return m, tea.Quit
			} else {
				m.mode = modeFilePicker
				m.errorMessage = ""
			}
		}

	case "?", "u":
		m.showHelp = !m.showHelp

	case "up", "k":
		m.viewport.LineUp(1)
		m.saveCurrentPosition()

	case "down", "j":
		m.viewport.LineDown(1)
		m.saveCurrentPosition()

	case "ctrl+u":
		m.viewport.HalfPageUp()
		m.saveCurrentPosition()

	case "ctrl+d":
		m.viewport.HalfPageDown()
		m.saveCurrentPosition()

	case "g":
		m.lastKey = "g"

	case "G":
		m.viewport.GotoBottom()
		m.saveCurrentPosition()

	case "left", "h", "p":
		if m.currentChapterIndex > 0 {
			m.stopTTS()
			m.currentChapterIndex--
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchActiveIndex = -1
			m.viewport.GotoTop()
			m.saveCurrentPosition()
			if m.showingTranslation {
				if _, ok := m.translatedChapters[m.currentChapterIndex]; !ok && !m.translating {
					m.translating = true
					m.translationError = ""
					return m, translateChapterCmd(m.currentChapterIndex, m.chapters[m.currentChapterIndex].Body)
				}
			}
			m.viewport.SetContent(m.getProcessedChapterContent())
		}

	case "right", "l", "space":
		if m.currentChapterIndex < len(m.chapters)-1 {
			m.stopTTS()
			m.currentChapterIndex++
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchActiveIndex = -1
			m.viewport.GotoTop()
			m.saveCurrentPosition()
			if m.showingTranslation {
				if _, ok := m.translatedChapters[m.currentChapterIndex]; !ok && !m.translating {
					m.translating = true
					m.translationError = ""
					return m, translateChapterCmd(m.currentChapterIndex, m.chapters[m.currentChapterIndex].Body)
				}
			}
			m.viewport.SetContent(m.getProcessedChapterContent())
		}

	case "t":
		m.stopTTS()
		if m.showingTranslation {
			m.showingTranslation = false
			m.translationError = ""
			m.viewport.SetContent(m.getProcessedChapterContent())
		} else {
			if translated, ok := m.translatedChapters[m.currentChapterIndex]; ok && translated != "" {
				m.showingTranslation = true
				m.translationError = ""
				m.viewport.SetContent(m.getProcessedChapterContent())
			} else if !m.translating {
				m.translating = true
				m.translationError = ""
				return m, translateChapterCmd(m.currentChapterIndex, m.chapters[m.currentChapterIndex].Body)
			}
		}

	case "s":
		if m.isSpeaking {
			m.stopTTS()
			m.ttsError = ""
		} else {
			cmd := m.startTTSCmd()
			return m, cmd
		}

	case "+", "]", ">":
		speed := m.getTTSSpeed() + 0.15
		if speed > 3.0 {
			speed = 3.0
		}
		m.ttsSpeed = speed
		if m.isSpeaking {
			return m, m.startTTSCmd()
		}

	case "-", "[", "<":
		speed := m.getTTSSpeed() - 0.15
		if speed < 0.5 {
			speed = 0.5
		}
		m.ttsSpeed = speed
		if m.isSpeaking {
			return m, m.startTTSCmd()
		}

	case "n", "ctrl+n":
		if len(m.searchMatches) > 0 {
			m.searchActiveIndex = (m.searchActiveIndex + 1) % len(m.searchMatches)
			m.scrollToMatch()
			m.viewport.SetContent(m.getProcessedChapterContent())
			m.saveCurrentPosition()
		}

	case "N", "ctrl+p":
		if len(m.searchMatches) > 0 {
			m.searchActiveIndex = (m.searchActiveIndex - 1 + len(m.searchMatches)) % len(m.searchMatches)
			m.scrollToMatch()
			m.viewport.SetContent(m.getProcessedChapterContent())
			m.saveCurrentPosition()
		}
	}

	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.searching = false
		m.updateSearchMatches()
		m.viewport.SetContent(m.getProcessedChapterContent())
		m.saveCurrentPosition()
		return m, nil

	case tea.KeyEsc, tea.KeyCtrlC:
		m.searching = false
		m.readerSearchInput.SetValue("")
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchActiveIndex = -1
		m.viewport.SetContent(m.getProcessedChapterContent())
		return m, nil

	case tea.KeyCtrlN:
		m.updateSearchMatches()
		if len(m.searchMatches) > 0 {
			m.searchActiveIndex = (m.searchActiveIndex + 1) % len(m.searchMatches)
			m.scrollToMatch()
			m.viewport.SetContent(m.getProcessedChapterContent())
			m.saveCurrentPosition()
		}
		return m, nil

	case tea.KeyCtrlP:
		m.updateSearchMatches()
		if len(m.searchMatches) > 0 {
			m.searchActiveIndex = (m.searchActiveIndex - 1 + len(m.searchMatches)) % len(m.searchMatches)
			m.scrollToMatch()
			m.viewport.SetContent(m.getProcessedChapterContent())
			m.saveCurrentPosition()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.readerSearchInput, cmd = m.readerSearchInput.Update(msg)
	m.searchQuery = m.readerSearchInput.Value()
	m.updateSearchMatches()
	m.viewport.SetContent(m.getProcessedChapterContent())
	return m, cmd
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	switch m.mode {
	case modeFilePicker:
		return m.viewFilePicker()
	case modeReader:
		return m.viewReader()
	}
	return ""
}

func (m model) viewFilePicker() string {
	var s strings.Builder

	s.WriteString(styleFilePickerHeader.Render("🔍 TUI E-Reader - Search Books"))
	s.WriteString("\n\n")

	// Search bar at the top (FZF style)
	s.WriteString(fmt.Sprintf("  Search: %s\n\n", m.filePickerInput.View()))

	if m.loading {
		s.WriteString("  Searching for books with 'fd'...\n\n")
	} else if m.errorMessage != "" {
		s.WriteString(styleError.Render(fmt.Sprintf("Error: %s", m.errorMessage)))
		s.WriteString("\n\n")
	} else {
		s.WriteString(fmt.Sprintf("  Found %d books in $HOME\n\n", len(m.filteredFiles)))
	}

	if !m.loading && len(m.filteredFiles) == 0 {
		s.WriteString("  (No matches found)\n")
	} else {
		maxItems := m.height - 10
		if maxItems < 1 {
			maxItems = 1
		}
		start := 0
		end := len(m.filteredFiles)
		if len(m.filteredFiles) > maxItems {
			start = m.fileIndex - maxItems/2
			if start < 0 {
				start = 0
			}
			end = start + maxItems
			if end > len(m.filteredFiles) {
				end = len(m.filteredFiles)
				start = end - maxItems
				if start < 0 {
					start = 0
				}
			}
		}

		for idx := start; idx < end; idx++ {
			file := m.filteredFiles[idx]
			prefix := "  "
			if idx == m.fileIndex {
				prefix = "> "
			}

			icon := "▫️ "
			if strings.HasSuffix(strings.ToLower(file.name), ".epub") {
				icon = "📘 "
			} else if strings.HasSuffix(strings.ToLower(file.name), ".pdf") {
				icon = "📕 "
			}

			line := fmt.Sprintf("%s%s%s", prefix, icon, file.name)
			if idx == m.fileIndex {
				s.WriteString(styleFilePickerSelected.Render(line))
			} else {
				s.WriteString(line)
			}
			s.WriteString("\n")
		}
	}

	linesCount := strings.Count(s.String(), "\n")
	neededNewlines := m.height - linesCount - 2
	for i := 0; i < neededNewlines; i++ {
		s.WriteString("\n")
	}

	s.WriteString(styleFooter.Render("Ctrl-j/k or Arrows: Navigate  •  Enter: Open  •  Esc: Clear/Exit"))
	return s.String()
}

func (m model) viewReader() string {
	var s strings.Builder

	bookType := strings.ToUpper(m.metadata.Type)
	titleStr := styleTitle.Render(fmt.Sprintf("[%s] %s", bookType, m.metadata.Title))
	authorStr := styleAuthor.Render(fmt.Sprintf(" by %s", m.metadata.Author))
	chapterTitleStr := fmt.Sprintf("Chapter: %s", m.chapters[m.currentChapterIndex].Title)
	if m.showingTranslation {
		chapterTitleStr += " [VIETNAMESE]"
	}

	s.WriteString(styleHeader.Render(fmt.Sprintf("%s%s\n%s", titleStr, authorStr, chapterTitleStr)))
	s.WriteString("\n\n")

	if m.showHelp {
		s.WriteString(m.viewHelp())
	} else {
		s.WriteString(m.viewport.View())
	}
	s.WriteString("\n\n")

	if m.searching {
		s.WriteString(fmt.Sprintf("/%s", m.readerSearchInput.View()))
	} else {
		pct := 0
		if len(m.chapters) > 0 {
			pct = int(float64(m.currentChapterIndex) / float64(len(m.chapters)) * 100)
		}

		progressText := styleProgress.Render(fmt.Sprintf("Progress: %d/%d (%d%%)", m.currentChapterIndex+1, len(m.chapters), pct))

		var translateStatus string
		if m.translating {
			translateStatus = " | ⏳ Translating to Vietnamese..."
		} else if m.showingTranslation {
			translateStatus = " | [VN Mode: Press 't' for Original]"
		} else if m.translationError != "" {
			translateStatus = fmt.Sprintf(" | %s", styleError.Render(m.translationError))
		} else {
			translateStatus = " | [t: Translate to VN]"
		}

		var ttsStatus string
		speedStr := fmt.Sprintf("%.2fx", m.getTTSSpeed())
		if m.isSpeaking {
			ttsStatus = fmt.Sprintf(" | 🔊 Speaking (%s)... [s: Stop, +/-: Speed]", speedStr)
		} else if m.ttsError != "" {
			ttsStatus = fmt.Sprintf(" | %s", styleError.Render(m.ttsError))
		} else {
			ttsStatus = fmt.Sprintf(" | [s: Speak (%s)]", speedStr)
		}

		var searchText string
		if m.searchQuery != "" {
			matchIndexText := "no matches"
			if len(m.searchMatches) > 0 {
				matchIndexText = fmt.Sprintf("%d/%d", m.searchActiveIndex+1, len(m.searchMatches))
			}
			searchText = fmt.Sprintf(" | Search: \"%s\" (%s, n/N)", m.searchQuery, matchIndexText)
		}

		s.WriteString(styleFooter.Render(fmt.Sprintf("%s%s%s%s  •  [? / u: Help]", progressText, translateStatus, ttsStatus, searchText)))
	}

	return s.String()
}

func (m model) viewHelp() string {
	var s strings.Builder
	s.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Background(lipgloss.Color("#7D56F4")).Bold(true).Padding(0, 2).Render("HELP & KEYBINDINGS"))
	s.WriteString("\n\n")

	keys := [][]string{
		{"j, Down", "Scroll down 1 line"},
		{"k, Up", "Scroll up 1 line"},
		{"ctrl+d", "Scroll down half page"},
		{"ctrl+u", "Scroll up half page"},
		{"gg", "Go to Top of chapter"},
		{"G", "Go to Bottom of chapter"},
		{"l, Right, Space", "Next Chapter / Page"},
		{"h, Left, p", "Previous Chapter / Page"},
		{"t", "Toggle Vietnamese translation"},
		{"s", "Toggle Text-to-Speech (read aloud)"},
		{"+ / - or ] / [", "Increase / decrease speech speed"},
		{"/", "Search within chapter"},
		{"n", "Next search match"},
		{"N", "Previous search match"},
		{"?, u", "Toggle Help screen"},
		{"q", "Go back to search list / Exit"},
		{"ctrl+c", "Quit program"},
	}

	for _, k := range keys {
		s.WriteString(fmt.Sprintf("  %s %s\n", styleHelpKey.Render(fmt.Sprintf("%-16s", k[0])), styleHelpDesc.Render(k[1])))
	}

	linesCount := strings.Count(s.String(), "\n")
	neededNewlines := m.viewport.Height - linesCount
	for i := 0; i < neededNewlines; i++ {
		s.WriteString("\n")
	}

	return s.String()
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func main() {
	var initialModel model
	initialModel.loading = true

	initialModel.filePickerInput = textinput.New()
	initialModel.filePickerInput.Placeholder = ""
	initialModel.filePickerInput.Focus()
	initialModel.filePickerInput.Prompt = ""

	initialModel.readerSearchInput = textinput.New()
	initialModel.readerSearchInput.Placeholder = ""
	initialModel.readerSearchInput.Prompt = ""

	if len(os.Args) > 1 {
		filePath := os.Args[1]
		if filePath == "-h" || filePath == "--help" {
			fmt.Printf("Usage: %s [epub_or_pdf_file]\n", os.Args[0])
			os.Exit(0)
		}

		info, err := os.Stat(filePath)
		if err != nil {
			fmt.Printf("Error: File not found: %s\n", filePath)
			os.Exit(1)
		}

		if info.IsDir() {
			initialModel.cliMode = false
		} else {
			initialModel.cliMode = true
			err = initialModel.loadBook(filePath)
			if err != nil {
				fmt.Printf("Error loading file: %s\n", err)
				os.Exit(1)
			}
		}
	} else {
		initialModel.cliMode = false
	}

	p := tea.NewProgram(initialModel, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v\n", err)
		os.Exit(1)
	}
}
