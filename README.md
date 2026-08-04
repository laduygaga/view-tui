# View TUI

A terminal-based E-Reader for EPUB and PDF files built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). It features a lightning-fast, `fzf`-style fuzzy book searcher powered by `fd` and a highly readable Vim-like paginated text viewer.

## Features

- **Format Support**: Read `.epub` (HTML-stripped) and `.pdf` (text extracted page-by-page) files natively.
- **FZF-Style Book Picker**: Instant `fd` search starting from `$HOME` with standard exclusions (`.git`, `node_modules`, etc.).
- **Immediate Typing**: Simply launch and type to filter books instantly—no extra keys required.
- **Readline / Shell Editing**: Full support for standard editing shortcuts (`Ctrl-a`, `Ctrl-e`, `Ctrl-u`, `Ctrl-k`, `Ctrl-w`, and arrow keys) in all search fields.
- **Vim Navigation**: Scroll, read, and flip chapters easily with familiar Vim shortcuts.
- **Vietnamese Translation**: Press `t` while reading to translate any chapter or page to Vietnamese instantly, with response caching and seamless toggling back to original text.
- **High Readability**: Automatic word wrapping with elegant padding margins on both sides for a clean e-reader feel.

## Installation

Ensure you have [Go](https://go.dev/) and [`fd`](https://github.com/sharkdp/fd) installed, then:

```bash
git clone git@github.com:laduygaga/view-tui.git
cd view-tui
go build -o view-tui
```

## Usage

Start the fuzzy book searcher:
```bash
./view-tui
```

Or open a specific book directly:
```bash
./view-tui path/to/book.epub
```

---

## Keybindings

### 🔍 Book Searcher (FZF-Style)

| Key | Action |
| --- | --- |
| `Type any characters` | Type immediately to search/filter books |
| `Ctrl-j` / `Ctrl-n` / `Down` | Move selection down |
| `Ctrl-k` / `Ctrl-p` / `Up` | Move selection up |
| `Enter` | Open selected book |
| `Esc` | Clear search input / Exit |
| `r` (when search is empty) | Re-run global `fd` file discovery |
| `q` (when search is empty) | Quit the application |

### 📖 E-Book Reader (Vim-Style)

| Key | Action |
| --- | --- |
| `j` / `Down` | Scroll down 1 line |
| `k` / `Up` | Scroll up 1 line |
| `ctrl+d` / `ctrl+u` | Scroll down / up half-page |
| `gg` | Jump to top of current chapter/page |
| `G` | Jump to bottom of current chapter/page |
| `l` / `Right` / `Space` | Next chapter (EPUB) or page (PDF) |
| `h` / `Left` / `p` | Previous chapter (EPUB) or page (PDF) |
| `t` | Toggle Vietnamese translation |
| `/` | Search/highlight text inside current chapter/page |
| `n` / `Ctrl-n` | Next search match (auto-scrolls to center) |
| `N` / `Ctrl-p` | Previous search match (auto-scrolls to center) |
| `?` / `u` | Toggle help overlay |
| `q` | Go back to Searcher / Exit |

---

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- [dslipak/pdf](https://github.com/dslipak/pdf)
- [taylorskalyo/goreader](https://github.com/taylorskalyo/goreader)

## License

MIT
