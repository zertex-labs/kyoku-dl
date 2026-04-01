package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AlbumChoice represents a selectable album in the artist album list
type AlbumChoice struct {
	ID           string
	Title        string
	Artist       string
	ReleaseType  string // Album, Single, EP, Compilation
	ReleaseDate  string
	TrackCount   int
	AudioQuality string
	Selected     bool
}

// releaseSection groups albums by type for display
type releaseSection struct {
	Title   string // e.g. "Albums", "EP & Singles", "Compilations"
	Indices []int  // indices into the flat Albums slice
}

// sectionOrder defines the display order and grouping of release types.
// Tidal groups EPs and Singles together under "EP & Singles".
var sectionOrder = []struct {
	title string
	types []string // ReleaseType values that belong to this section
}{
	{"Albums", []string{"Album", ""}},
	{"EP & Singles", []string{"Single", "EP"}},
	{"Compilations", []string{"Compilation"}},
}

// SelectorModel is the bubbletea model for selecting albums to download
type SelectorModel struct {
	// Content info
	ArtistName string
	Albums     []AlbumChoice

	// Sections for grouped display
	sections []releaseSection

	// Cursor position (index into flat Albums slice)
	cursor int

	// Viewport / scrolling
	termHeight int // terminal height (0 = unknown)
	scrollOff  int // first visible line index in the items area

	// State
	confirmed bool
	cancelled bool
}

// SelectorResult holds the result of the selection
type SelectorResult struct {
	Selected  []AlbumChoice
	Cancelled bool
}

// NewSelectorModel creates a new album selector model
func NewSelectorModel(artistName string, albums []AlbumChoice) *SelectorModel {
	// Select all by default
	for i := range albums {
		albums[i].Selected = true
	}

	// Build sections by grouping albums by release type
	sections := buildSections(albums)

	// Set initial cursor to first item of first non-empty section
	cursor := 0
	if len(sections) > 0 && len(sections[0].Indices) > 0 {
		cursor = sections[0].Indices[0]
	}

	return &SelectorModel{
		ArtistName: artistName,
		Albums:     albums,
		sections:   sections,
		cursor:     cursor,
	}
}

// buildSections groups album indices by release type sections.
func buildSections(albums []AlbumChoice) []releaseSection {
	// Build a map of release type -> indices
	typeMap := make(map[string][]int)
	for i, a := range albums {
		typeMap[a.ReleaseType] = append(typeMap[a.ReleaseType], i)
	}

	var sections []releaseSection
	for _, so := range sectionOrder {
		var indices []int
		for _, t := range so.types {
			indices = append(indices, typeMap[t]...)
			delete(typeMap, t)
		}
		if len(indices) > 0 {
			sections = append(sections, releaseSection{
				Title:   so.title,
				Indices: indices,
			})
		}
	}

	// Catch any remaining types not in sectionOrder (e.g. unknown types)
	var otherIndices []int
	for _, indices := range typeMap {
		otherIndices = append(otherIndices, indices...)
	}
	if len(otherIndices) > 0 {
		sections = append(sections, releaseSection{
			Title:   "Other",
			Indices: otherIndices,
		})
	}

	return sections
}

// orderedIndices returns all album indices in section display order.
func (m *SelectorModel) orderedIndices() []int {
	var indices []int
	for _, s := range m.sections {
		indices = append(indices, s.Indices...)
	}
	return indices
}

// Init initializes the model
func (m *SelectorModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *SelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termHeight = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			m.moveCursor(-1)
			m.ensureCursorVisible()

		case "down", "j":
			m.moveCursor(1)
			m.ensureCursorVisible()

		case " ", "x":
			// Toggle selection
			if m.cursor >= 0 && m.cursor < len(m.Albums) {
				m.Albums[m.cursor].Selected = !m.Albums[m.cursor].Selected
			}

		case "a":
			// Toggle all: if all selected, deselect all; otherwise select all
			allSelected := true
			for _, a := range m.Albums {
				if !a.Selected {
					allSelected = false
					break
				}
			}
			for i := range m.Albums {
				m.Albums[i].Selected = !allSelected
			}

		case "enter":
			m.confirmed = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// moveCursor moves the cursor up or down through the sectioned list.
func (m *SelectorModel) moveCursor(direction int) {
	ordered := m.orderedIndices()
	if len(ordered) == 0 {
		return
	}

	// Find current position in ordered list
	currentPos := -1
	for i, idx := range ordered {
		if idx == m.cursor {
			currentPos = i
			break
		}
	}

	newPos := currentPos + direction
	if newPos < 0 {
		newPos = 0
	}
	if newPos >= len(ordered) {
		newPos = len(ordered) - 1
	}

	m.cursor = ordered[newPos]
}

// buildItemLines pre-renders each section header and album line, returning
// them along with the album index each line corresponds to (-1 for headers/blanks).
type itemLine struct {
	text     string
	albumIdx int // -1 = non-selectable (header, blank)
}

func (m *SelectorModel) buildItemLines() []itemLine {
	// Styles (same as View)
	checkboxSelected := lipgloss.NewStyle().Foreground(Success).Bold(true)
	checkboxEmpty := lipgloss.NewStyle().Foreground(Muted)
	cursorStyle := lipgloss.NewStyle().Foreground(Primary).Bold(true)
	albumTitleActive := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true)
	albumTitleInactive := lipgloss.NewStyle().Foreground(Muted)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(Secondary)

	var lines []itemLine

	for si, section := range m.sections {
		// Blank line before section (except the first)
		if si > 0 {
			lines = append(lines, itemLine{"", -1})
		}
		// Section header
		lines = append(lines, itemLine{sectionStyle.Render(section.Title), -1})

		for _, idx := range section.Indices {
			album := m.Albums[idx]

			cursor := "  "
			if idx == m.cursor {
				cursor = cursorStyle.Render("› ")
			}

			var checkbox string
			if album.Selected {
				checkbox = checkboxSelected.Render("[✓]")
			} else {
				checkbox = checkboxEmpty.Render("[ ]")
			}

			var title string
			if album.Selected {
				title = albumTitleActive.Render(album.Title)
			} else {
				title = albumTitleInactive.Render(album.Title)
			}

			details := []string{}
			if album.ReleaseDate != "" {
				year := album.ReleaseDate
				if len(year) >= 4 {
					year = year[:4]
				}
				details = append(details, year)
			}
			if album.TrackCount > 0 {
				details = append(details, fmt.Sprintf("%d tracks", album.TrackCount))
			}

			detailStr := ""
			if len(details) > 0 {
				detailStr = " " + MutedStyle.Render("("+strings.Join(details, " · ")+")")
			}

			line := fmt.Sprintf("%s%s %s%s", cursor, checkbox, title, detailStr)
			lines = append(lines, itemLine{line, idx})
		}
	}

	return lines
}

// cursorLineIndex returns the line index in the items list that contains the
// current cursor album.
func (m *SelectorModel) cursorLineIndex(lines []itemLine) int {
	for i, l := range lines {
		if l.albumIdx == m.cursor {
			return i
		}
	}
	return 0
}

// visibleItemLines returns how many item lines fit in the terminal after
// reserving space for the header (3 lines) and footer (3 lines).
func (m *SelectorModel) visibleItemLines() int {
	if m.termHeight <= 0 {
		return 9999 // no limit if unknown
	}
	reserved := 6 // header (3) + footer help (3)
	avail := m.termHeight - reserved
	if avail < 3 {
		avail = 3
	}
	return avail
}

// ensureCursorVisible adjusts scrollOff so the cursor line is within the
// visible window.
func (m *SelectorModel) ensureCursorVisible() {
	lines := m.buildItemLines()
	ci := m.cursorLineIndex(lines)
	vis := m.visibleItemLines()

	if ci < m.scrollOff {
		m.scrollOff = ci
	}
	if ci >= m.scrollOff+vis {
		m.scrollOff = ci - vis + 1
	}
	// clamp
	maxOff := len(lines) - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if m.scrollOff > maxOff {
		m.scrollOff = maxOff
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// View renders the selector UI
func (m *SelectorModel) View() string {
	if m.cancelled {
		return MutedStyle.Render("Selection cancelled.\n")
	}

	if m.confirmed {
		return "" // Will be cleared by the caller
	}

	var b strings.Builder

	// Header
	artistIcon := "🎤"
	header := fmt.Sprintf("%s %s", artistIcon, TitleStyle.Render(m.ArtistName))
	b.WriteString(header + "\n")

	// Count
	selectedCount := 0
	for _, a := range m.Albums {
		if a.Selected {
			selectedCount++
		}
	}
	countStr := fmt.Sprintf("%d/%d releases selected", selectedCount, len(m.Albums))
	b.WriteString(MutedStyle.Render(countStr) + "\n")

	// Build all item lines
	lines := m.buildItemLines()

	// Compute visible window
	vis := m.visibleItemLines()
	m.ensureCursorVisible() // re-clamp in case terminal resized

	start := m.scrollOff
	end := start + vis
	if end > len(lines) {
		end = len(lines)
	}

	// Scroll indicators
	if start > 0 {
		b.WriteString(MutedStyle.Render("  ↑ more") + "\n")
	} else {
		b.WriteString("\n")
	}

	for i := start; i < end; i++ {
		b.WriteString(lines[i].text + "\n")
	}

	if end < len(lines) {
		b.WriteString(MutedStyle.Render("  ↓ more") + "\n")
	} else {
		b.WriteString("\n")
	}

	// Help
	helpParts := []string{
		"↑/↓ navigate",
		"space toggle",
		"a select/deselect all",
		"enter confirm",
		"q cancel",
	}
	b.WriteString(HelpStyle.Render(strings.Join(helpParts, " • ")))
	b.WriteString("\n")

	return b.String()
}

// Result returns the selection result
func (m *SelectorModel) Result() SelectorResult {
	if m.cancelled {
		return SelectorResult{Cancelled: true}
	}

	var selected []AlbumChoice
	for _, a := range m.Albums {
		if a.Selected {
			selected = append(selected, a)
		}
	}

	return SelectorResult{
		Selected:  selected,
		Cancelled: false,
	}
}

// RunSelector runs the album selector TUI and returns the selected albums.
// Returns the selected albums and whether the selection was cancelled.
func RunSelector(artistName string, albums []AlbumChoice) ([]AlbumChoice, bool, error) {
	model := NewSelectorModel(artistName, albums)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return nil, false, fmt.Errorf("selector TUI error: %w", err)
	}

	m, ok := finalModel.(*SelectorModel)
	if !ok {
		return nil, false, fmt.Errorf("unexpected model type")
	}

	result := m.Result()
	return result.Selected, result.Cancelled, nil
}
