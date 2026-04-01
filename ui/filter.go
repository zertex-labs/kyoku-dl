package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ReleaseTypeOption represents a selectable release type category
type ReleaseTypeOption struct {
	Label    string   // Display label (e.g. "Albums")
	Types    []string // Tidal API filter values (e.g. ["ALBUMS"])
	Count    int      // How many releases of this type exist
	Selected bool
}

// FilterModel is the bubbletea model for selecting which release types to fetch
type FilterModel struct {
	ArtistName string
	Options    []ReleaseTypeOption
	cursor     int
	confirmed  bool
	cancelled  bool
}

// FilterResult holds the result of the release type filter selection
type FilterResult struct {
	// SelectedFilters contains the Tidal API filter values to fetch
	SelectedFilters []string
	Cancelled       bool
}

// NewFilterModel creates a new release type filter model.
// counts maps display labels to the number of releases of that type.
func NewFilterModel(artistName string, counts map[string]int) *FilterModel {
	options := []ReleaseTypeOption{
		{Label: "Albums", Types: []string{"ALBUMS"}, Count: counts["Albums"], Selected: true},
		{Label: "EP & Singles", Types: []string{"EPSANDSINGLES"}, Count: counts["EP & Singles"], Selected: true},
		{Label: "Compilations", Types: []string{"COMPILATIONS"}, Count: counts["Compilations"], Selected: false},
	}

	// Remove options with 0 releases
	var filtered []ReleaseTypeOption
	for _, opt := range options {
		if opt.Count > 0 {
			filtered = append(filtered, opt)
		}
	}

	return &FilterModel{
		ArtistName: artistName,
		Options:    filtered,
	}
}

func (m *FilterModel) Init() tea.Cmd {
	return nil
}

func (m *FilterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.Options)-1 {
				m.cursor++
			}

		case " ", "x":
			if m.cursor >= 0 && m.cursor < len(m.Options) {
				m.Options[m.cursor].Selected = !m.Options[m.cursor].Selected
			}

		case "a":
			allSelected := true
			for _, o := range m.Options {
				if !o.Selected {
					allSelected = false
					break
				}
			}
			for i := range m.Options {
				m.Options[i].Selected = !allSelected
			}

		case "enter":
			// Must have at least one selected
			hasSelection := false
			for _, o := range m.Options {
				if o.Selected {
					hasSelection = true
					break
				}
			}
			if hasSelection {
				m.confirmed = true
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

func (m *FilterModel) View() string {
	if m.cancelled {
		return MutedStyle.Render("Selection cancelled.\n")
	}
	if m.confirmed {
		return ""
	}

	var b strings.Builder

	// Header
	header := fmt.Sprintf("🎤 %s", TitleStyle.Render(m.ArtistName))
	b.WriteString(header + "\n")
	b.WriteString(MutedStyle.Render("Select which release types to include:") + "\n\n")

	// Styles
	checkboxSelected := lipgloss.NewStyle().Foreground(Success).Bold(true)
	checkboxEmpty := lipgloss.NewStyle().Foreground(Muted)
	cursorStyle := lipgloss.NewStyle().Foreground(Primary).Bold(true)
	labelActive := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true)
	labelInactive := lipgloss.NewStyle().Foreground(Muted)

	for i, opt := range m.Options {
		// Cursor
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("› ")
		}

		// Checkbox
		var checkbox string
		if opt.Selected {
			checkbox = checkboxSelected.Render("[✓]")
		} else {
			checkbox = checkboxEmpty.Render("[ ]")
		}

		// Label
		var label string
		if opt.Selected {
			label = labelActive.Render(opt.Label)
		} else {
			label = labelInactive.Render(opt.Label)
		}

		// Count
		count := MutedStyle.Render(fmt.Sprintf("(%d)", opt.Count))

		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, checkbox, label, count))
	}

	// Help
	b.WriteString("\n")

	// Check if at least one selected for enter hint
	hasSelection := false
	for _, o := range m.Options {
		if o.Selected {
			hasSelection = true
			break
		}
	}

	helpParts := []string{
		"↑/↓ navigate",
		"space toggle",
		"a select/deselect all",
	}
	if hasSelection {
		helpParts = append(helpParts, "enter confirm")
	} else {
		helpParts = append(helpParts, MutedStyle.Render("select at least one to continue"))
	}
	helpParts = append(helpParts, "q cancel")
	b.WriteString(HelpStyle.Render(strings.Join(helpParts, " • ")))
	b.WriteString("\n")

	return b.String()
}

func (m *FilterModel) Result() FilterResult {
	if m.cancelled {
		return FilterResult{Cancelled: true}
	}

	var filters []string
	for _, opt := range m.Options {
		if opt.Selected {
			filters = append(filters, opt.Types...)
		}
	}

	return FilterResult{
		SelectedFilters: filters,
		Cancelled:       false,
	}
}

// RunFilter runs the release type filter TUI and returns the selected API filters.
func RunFilter(artistName string, counts map[string]int) ([]string, bool, error) {
	model := NewFilterModel(artistName, counts)

	// If there's only one type with releases, skip the filter
	if len(model.Options) <= 1 {
		var filters []string
		for _, opt := range model.Options {
			filters = append(filters, opt.Types...)
		}
		return filters, false, nil
	}

	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return nil, false, fmt.Errorf("filter TUI error: %w", err)
	}

	m, ok := finalModel.(*FilterModel)
	if !ok {
		return nil, false, fmt.Errorf("unexpected model type")
	}

	result := m.Result()
	return result.SelectedFilters, result.Cancelled, nil
}
