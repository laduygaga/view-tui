# View TUI

A terminal-based E-Reader for EPUB and PDF files built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- **Format Support**: Read `.epub` and `.pdf` files directly in your terminal.
- **Vim Keybindings**: Efficient navigation using familiar shortcuts.
- **Search**: Interactive search (`/`) with highlighting and navigation (`n`/`N`).
- **File Explorer**: Built-in file picker to browse and open books.
- **Responsive Design**: Beautifully formatted text wrapping and status bars.

## Installation

Ensure you have Go installed, then:

```bash
git clone git@github.com:laduygaga/view-tui.git
cd view-tui
go build -o view-tui
```

## Usage

Start the reader by providing a file path:
```bash
./view-tui path/to/your/book.epub
```

Or start the file picker to browse your library:
```bash
./view-tui
```

## Keybindings

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll down / up |
| `ctrl+d` / `ctrl+u` | Half-page scroll down / up |
| `gg` / `G` | Jump to top / bottom |
| `h` / `l` | Previous / Next chapter or page |
| `/` | Search in current chapter |
| `n` / `N` | Next / Previous match |
| `?` / `u` | Toggle Help |
| `q` | Back to file list / Exit |

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- [dslipak/pdf](https://github.com/dslipak/pdf)
- [taylorskalyo/goreader](https://github.com/taylorskalyo/goreader)

## License

MIT
