package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dslipak/pdf"
	"github.com/taylorskalyo/goreader/epub"
	"golang.org/x/net/html"
)

// Chapter represents a chapter in EPUB or a page in PDF
type Chapter struct {
	Title string
	Body  string
}

// BookMetadata contains information about the book
type BookMetadata struct {
	Title  string
	Author string
	Type   string // "epub" or "pdf"
}

// appMode defines the application screen
type appMode int

const (
	modeFilePicker appMode = iota
	modeReader
)

// fileEntry represents an item in the directory list
type fileEntry struct {
	name  string
	isDir bool
	size  int64
}

// Lip Gloss Styles
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

	styleFileDir = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
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

// Model represents the bubble tea application state
type model struct {
	mode                appMode
	currentPath         string
	files               []fileEntry
	filteredFiles       []fileEntry
	fileIndex           int
	errorMessage        string
	cliMode             bool // true if path was passed as argument

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

	filePickerSearching    bool
	filePickerSearchQuery  string

	showHelp            bool
	lastKey             string
}

// htmlToTextAndTitle extracts text content and title from an HTML reader
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

// cleanText formats and normalizes whitespaces/HTML entities
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

// readEpub parses an EPUB file and extracts readable chapters and metadata
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

// readPdf parses a PDF file and extracts readable pages
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

	var chapters []Chapter
	numPages := r.NumPage()
	for i := 1; i <= numPages; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}

		rows, err := p.GetTextByRow()
		if err != nil {
			continue
		}

		var pageBuf strings.Builder
		for _, row := range rows {
			for _, word := range row.Content {
				pageBuf.WriteString(word.S)
			}
			pageBuf.WriteString("\n")
		}

		body := strings.TrimSpace(pageBuf.String())
		if body == "" {
			continue
		}

		chapters = append(chapters, Chapter{
			Title: fmt.Sprintf("Page %d", i),
			Body:  body,
		})
	}

	if len(chapters) == 0 {
		return nil, meta, fmt.Errorf("no readable text found in pdf")
	}

	return chapters, meta, nil
}

// refreshFiles reads current directory contents and filters directory listing
func (m *model) refreshFiles() error {
	absPath, err := filepath.Abs(m.currentPath)
	if err == nil {
		m.currentPath = absPath
	}

	entries, err := os.ReadDir(m.currentPath)
	if err != nil {
		return err
	}

	m.files = nil

	// Check if we can go up
	parent := filepath.Dir(m.currentPath)
	if parent != m.currentPath {
		m.files = append(m.files, fileEntry{
			name:  "..",
			isDir: true,
		})
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		isDir := entry.IsDir()
		ext := strings.ToLower(filepath.Ext(name))

		if isDir || ext == ".pdf" || ext == ".epub" {
			m.files = append(m.files, fileEntry{
				name:  name,
				isDir: isDir,
				size:  info.Size(),
			})
		}
	}

	m.filePickerSearchQuery = ""
	m.filePickerSearching = false
	m.filterFiles()

	return nil
}

func (m *model) filterFiles() {
	if m.filePickerSearchQuery == "" {
		m.filteredFiles = m.files
	} else {
		m.filteredFiles = nil
		query := strings.ToLower(m.filePickerSearchQuery)
		for _, f := range m.files {
			if f.name == ".." || strings.Contains(strings.ToLower(f.name), query) {
				m.filteredFiles = append(m.filteredFiles, f)
			}
		}
	}

	m.fileIndex = 0
	if len(m.filteredFiles) > 1 && m.filteredFiles[0].name == ".." {
		m.fileIndex = 1
	} else if len(m.filteredFiles) == 0 {
		m.fileIndex = 0
	}
}

// loadBook loads a book by filepath and sets reader mode
func (m *model) loadBook(filePath string) error {
	var chapters []Chapter
	var meta BookMetadata
	var err error

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

	m.chapters = chapters
	m.metadata = meta
	m.currentChapterIndex = 0
	m.mode = modeReader
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchActiveIndex = -1
	m.lastKey = ""
	m.showHelp = false

	// Initialize Viewport with vertical space reserved for header & footer
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
	m.viewport.GotoTop()

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

		for _, word := range words[1:] {
			if line.Len()-2+1+len(word) > targetWidth {
				wrappedParagraphs = append(wrappedParagraphs, line.String())
				line.Reset()
				line.WriteString("  ")
				line.WriteString(word)
			} else {
				line.WriteString(" ")
				line.WriteString(word)
			}
		}
		if line.Len() > 2 {
			wrappedParagraphs = append(wrappedParagraphs, line.String())
		}
	}

	return strings.Join(wrappedParagraphs, "\n")
}

// getProcessedChapterContent highlights text with active search queries
func (m *model) getProcessedChapterContent() string {
	if len(m.chapters) == 0 {
		return ""
	}
	body := m.chapters[m.currentChapterIndex].Body
	wrapped := wrapText(body, m.width)
	if m.searchQuery != "" {
		return highlightText(wrapped, m.searchQuery, m.searchActiveIndex, m.searchMatches)
	}
	return wrapped
}

// highlightText inserts Lip Gloss styles into matching search text
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

// updateSearchMatches searches for all query matches in current chapter
func (m *model) updateSearchMatches() {
	if m.searchQuery == "" {
		m.searchMatches = nil
		m.searchActiveIndex = -1
		return
	}

	body := m.chapters[m.currentChapterIndex].Body
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

// scrollToMatch scrolls the viewport to center the current active match
func (m *model) scrollToMatch() {
	if m.searchActiveIndex < 0 || m.searchActiveIndex >= len(m.searchMatches) {
		return
	}

	body := m.chapters[m.currentChapterIndex].Body
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

// Bubble Tea Model Methods
func (m model) Init() tea.Cmd {
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
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
			m.viewport.SetContent(m.getProcessedChapterContent())
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
	if m.filePickerSearching {
		switch msg.Type {
		case tea.KeyEnter:
			m.filePickerSearching = false
			return m, nil
		case tea.KeyEsc, tea.KeyCtrlC:
			m.filePickerSearching = false
			m.filePickerSearchQuery = ""
			m.filterFiles()
			return m, nil
		case tea.KeyBackspace:
			if len(m.filePickerSearchQuery) > 0 {
				m.filePickerSearchQuery = m.filePickerSearchQuery[:len(m.filePickerSearchQuery)-1]
				m.filterFiles()
			}
			return m, nil
		case tea.KeyRunes, tea.KeySpace:
			m.filePickerSearchQuery += string(msg.Runes)
			m.filterFiles()
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "/":
		m.filePickerSearching = true
		return m, nil

	case "up", "k":
		if len(m.filteredFiles) > 0 {
			m.fileIndex = (m.fileIndex - 1 + len(m.filteredFiles)) % len(m.filteredFiles)
		}

	case "down", "j":
		if len(m.filteredFiles) > 0 {
			m.fileIndex = (m.fileIndex + 1) % len(m.filteredFiles)
		}

	case "left", "h":
		parent := filepath.Dir(m.currentPath)
		if parent != m.currentPath {
			m.currentPath = parent
			m.refreshFiles()
		}

	case "right", "l", "enter":
		if len(m.filteredFiles) == 0 {
			break
		}
		selected := m.filteredFiles[m.fileIndex]
		if selected.isDir {
			if selected.name == ".." {
				m.currentPath = filepath.Dir(m.currentPath)
			} else {
				m.currentPath = filepath.Join(m.currentPath, selected.name)
			}
			m.refreshFiles()
		} else {
			filePath := filepath.Join(m.currentPath, selected.name)
			err := m.loadBook(filePath)
			if err != nil {
				m.errorMessage = err.Error()
			} else {
				m.errorMessage = ""
			}
		}
	}
	return m, nil
}

func (m model) updateReader(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		return m.updateSearch(msg)
	}

	keyStr := msg.String()

	// Handle 'gg' sequence
	if m.lastKey == "g" && keyStr == "g" {
		m.viewport.GotoTop()
		m.lastKey = ""
		return m, nil
	}
	if keyStr != "g" {
		m.lastKey = ""
	}

	switch keyStr {
	case "ctrl+c":
		return m, tea.Quit

	case "q":
		if m.showHelp {
			m.showHelp = false
		} else {
			if m.cliMode {
				return m, tea.Quit
			} else {
				m.mode = modeFilePicker
				m.errorMessage = ""
				m.refreshFiles()
			}
		}

	case "?", "u":
		m.showHelp = !m.showHelp

	case "up", "k":
		m.viewport.LineUp(1)

	case "down", "j":
		m.viewport.LineDown(1)

	case "ctrl+u":
		m.viewport.HalfPageUp()

	case "ctrl+d":
		m.viewport.HalfPageDown()

	case "g":
		m.lastKey = "g"

	case "G":
		m.viewport.GotoBottom()

	case "left", "h", "p":
		if m.currentChapterIndex > 0 {
			m.currentChapterIndex--
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchActiveIndex = -1
			m.viewport.SetContent(m.getProcessedChapterContent())
			m.viewport.GotoTop()
		}

	case "right", "l", "space":
		if m.currentChapterIndex < len(m.chapters)-1 {
			m.currentChapterIndex++
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchActiveIndex = -1
			m.viewport.SetContent(m.getProcessedChapterContent())
			m.viewport.GotoTop()
		}

	case "/":
		m.searching = true
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchActiveIndex = -1
		m.viewport.SetContent(m.getProcessedChapterContent())

	case "n":
		if len(m.searchMatches) > 0 {
			m.searchActiveIndex = (m.searchActiveIndex + 1) % len(m.searchMatches)
			m.scrollToMatch()
			m.viewport.SetContent(m.getProcessedChapterContent())
		}

	case "N":
		if len(m.searchMatches) > 0 {
			m.searchActiveIndex = (m.searchActiveIndex - 1 + len(m.searchMatches)) % len(m.searchMatches)
			m.scrollToMatch()
			m.viewport.SetContent(m.getProcessedChapterContent())
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

	case tea.KeyEsc, tea.KeyCtrlC:
		m.searching = false
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchActiveIndex = -1
		m.viewport.SetContent(m.getProcessedChapterContent())

	case tea.KeyBackspace:
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.updateSearchMatches()
			m.viewport.SetContent(m.getProcessedChapterContent())
		}

	case tea.KeyRunes, tea.KeySpace:
		m.searchQuery += string(msg.Runes)
		m.updateSearchMatches()
		m.viewport.SetContent(m.getProcessedChapterContent())
	}
	return m, nil
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

	// Header
	s.WriteString(styleFilePickerHeader.Render("📁 TUI E-Reader - Choose E-Book"))
	s.WriteString("\n\n")
	s.WriteString(fmt.Sprintf("Current Directory: %s\n\n", m.currentPath))

	if m.errorMessage != "" {
		s.WriteString(styleError.Render(fmt.Sprintf("Error: %s", m.errorMessage)))
		s.WriteString("\n\n")
	}

	// File List
	if len(m.filteredFiles) == 0 {
		s.WriteString("  (No EPUB or PDF files found in this folder)\n")
	} else {
		for i, file := range m.filteredFiles {
			prefix := "  "
			if i == m.fileIndex {
				prefix = "> "
			}

			icon := "▫️ "
			nameStr := file.name
			if file.isDir {
				icon = "📁 "
				nameStr = styleFileDir.Render(file.name)
			} else if strings.HasSuffix(strings.ToLower(file.name), ".epub") {
				icon = "📘 "
			} else if strings.HasSuffix(strings.ToLower(file.name), ".pdf") {
				icon = "📕 "
			}

			var sizeStr string
			if !file.isDir {
				sizeStr = fmt.Sprintf(" (%s)", formatBytes(file.size))
			}

			line := fmt.Sprintf("%s%s%s%s", prefix, icon, nameStr, sizeStr)

			if i == m.fileIndex {
				s.WriteString(styleFilePickerSelected.Render(line))
			} else {
				s.WriteString(line)
			}
			s.WriteString("\n")
		}
	}

	// Pad file picker to fill screen height
	linesCount := strings.Count(s.String(), "\n")
	neededNewlines := m.height - linesCount - 4
	for i := 0; i < neededNewlines; i++ {
		s.WriteString("\n")
	}

	if m.filePickerSearching {
		s.WriteString(fmt.Sprintf("Search: /%s█\n", m.filePickerSearchQuery))
	} else if m.filePickerSearchQuery != "" {
		s.WriteString(fmt.Sprintf("Search: /%s (Esc to clear)\n", m.filePickerSearchQuery))
	} else {
		s.WriteString("\n")
	}

	// Footer Keybindings
	s.WriteString(styleFooter.Render("j/k: Navigate  •  l/Enter: Open  •  /: Search  •  h: Back  •  q: Exit"))
	return s.String()
}

func (m model) viewReader() string {
	var s strings.Builder

	// Header Rendering
	bookType := strings.ToUpper(m.metadata.Type)
	titleStr := styleTitle.Render(fmt.Sprintf("[%s] %s", bookType, m.metadata.Title))
	authorStr := styleAuthor.Render(fmt.Sprintf(" by %s", m.metadata.Author))
	chapterTitleStr := fmt.Sprintf("Chapter: %s", m.chapters[m.currentChapterIndex].Title)

	s.WriteString(styleHeader.Render(fmt.Sprintf("%s%s\n%s", titleStr, authorStr, chapterTitleStr)))
	s.WriteString("\n\n")

	// Viewport Content or Help Menu
	if m.showHelp {
		s.WriteString(m.viewHelp())
	} else {
		s.WriteString(m.viewport.View())
	}
	s.WriteString("\n\n")

	// Footer Progress/Status or Search Bar
	if m.searching {
		s.WriteString(fmt.Sprintf("/%s█", m.searchQuery))
	} else {
		pct := 0
		if len(m.chapters) > 0 {
			pct = int(float64(m.currentChapterIndex) / float64(len(m.chapters)) * 100)
		}
		
		progressText := styleProgress.Render(fmt.Sprintf("Progress: %d/%d (%d%%)", m.currentChapterIndex+1, len(m.chapters), pct))
		
		var searchText string
		if m.searchQuery != "" {
			matchIndexText := "no matches"
			if len(m.searchMatches) > 0 {
				matchIndexText = fmt.Sprintf("%d/%d", m.searchActiveIndex+1, len(m.searchMatches))
			}
			searchText = fmt.Sprintf(" | Search: \"%s\" (%s, n/N)", m.searchQuery, matchIndexText)
		}

		s.WriteString(styleFooter.Render(fmt.Sprintf("%s%s  •  [? / u: Help]", progressText, searchText)))
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
		{"/", "Search within chapter"},
		{"n", "Next search match"},
		{"N", "Previous search match"},
		{"?, u", "Toggle Help screen"},
		{"q", "Go back to file list / Exit"},
		{"ctrl+c", "Quit program"},
	}

	for _, k := range keys {
		s.WriteString(fmt.Sprintf("  %s %s\n", styleHelpKey.Render(fmt.Sprintf("%-16s", k[0])), styleHelpDesc.Render(k[1])))
	}

	// Pad help screen to fill viewport height
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
	initialModel.currentPath = "."

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
			initialModel.currentPath = filePath
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

	if !initialModel.cliMode {
		err := initialModel.refreshFiles()
		if err != nil {
			fmt.Printf("Error scanning directory: %s\n", err)
			os.Exit(1)
		}
	}

	p := tea.NewProgram(initialModel, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v\n", err)
		os.Exit(1)
	}
}
