package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputModel is a bubbletea model for collecting URL input from the user.
type InputModel struct {
	textInput textinput.Model
	urls      []string
	cancelled bool
}

// NewInputModel creates a new interactive URL input model.
func NewInputModel() *InputModel {
	ti := textinput.New()
	ti.Placeholder = "https://tidal.com/browse/..."
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80
	ti.PromptStyle = TitleStyle
	ti.TextStyle = TrackTitleStyle

	return &InputModel{
		textInput: ti,
	}
}

// Init initializes the input model.
func (m *InputModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages for the input model.
func (m *InputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			value := strings.TrimSpace(m.textInput.Value())
			if value != "" {
				m.urls = strings.Fields(value)
			}
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// View renders the input UI.
func (m *InputModel) View() string {
	if m.cancelled {
		return ""
	}

	var b strings.Builder

	b.WriteString(TitleStyle.Render("kyoku-dl"))
	b.WriteString("\n\n")
	b.WriteString(SubtitleStyle.Render("Enter URL(s)"))
	b.WriteString(" ")
	b.WriteString(MutedStyle.Render("— paste one or more URLs separated by spaces"))
	b.WriteString("\n\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")
	b.WriteString(HelpStyle.Render("Enter to download • Esc to cancel"))

	return b.String()
}

// URLs returns the parsed URLs from user input.
func (m *InputModel) URLs() []string {
	return m.urls
}

// Cancelled returns whether the user cancelled input.
func (m *InputModel) Cancelled() bool {
	return m.cancelled
}

// RunInput launches the interactive URL input TUI and returns the entered URLs.
// Returns nil, nil if the user cancels.
func RunInput() ([]string, error) {
	model := NewInputModel()
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	m, ok := finalModel.(*InputModel)
	if !ok {
		return nil, nil
	}

	if m.Cancelled() {
		return nil, nil
	}

	return m.URLs(), nil
}
