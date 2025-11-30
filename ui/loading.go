package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// LoadingModel displays a loading spinner with a message
type LoadingModel struct {
	spinner spinner.Model
	message string
	detail  string
	done    bool
	err     error
}

// LoadingCompleteMsg signals that loading is complete
type LoadingCompleteMsg struct {
	Error error
}

// NewLoadingModel creates a new loading spinner model
func NewLoadingModel(message string) *LoadingModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = SpinnerStyle

	return &LoadingModel{
		spinner: s,
		message: message,
	}
}

// Init initializes the loading model
func (m *LoadingModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update handles messages
func (m *LoadingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case LoadingCompleteMsg:
		m.done = true
		m.err = msg.Error
		return m, tea.Quit
	}

	return m, nil
}

// View renders the loading spinner
func (m *LoadingModel) View() string {
	if m.done {
		if m.err != nil {
			return ErrorStyle.Render(fmt.Sprintf("%s Error: %v\n", IconCross, m.err))
		}
		return ""
	}

	var b strings.Builder

	// Spinner and message
	b.WriteString(m.spinner.View())
	b.WriteString(" ")
	b.WriteString(TitleStyle.Render(m.message))
	b.WriteString("\n")

	// Detail text if present
	if m.detail != "" {
		b.WriteString("  ")
		b.WriteString(MutedStyle.Render(m.detail))
		b.WriteString("\n")
	}

	b.WriteString(HelpStyle.Render("\nPress q to cancel"))

	return b.String()
}

// RunLoading runs a loading spinner while executing a function
func RunLoading(message string, fn func() error) error {
	model := NewLoadingModel(message)
	p := tea.NewProgram(model)

	// Run the function in a goroutine
	go func() {
		err := fn()
		p.Send(LoadingCompleteMsg{Error: err})
	}()

	// Run the TUI
	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	// Check if there was an error from the function
	if m, ok := finalModel.(*LoadingModel); ok && m.err != nil {
		return m.err
	}

	return nil
}
