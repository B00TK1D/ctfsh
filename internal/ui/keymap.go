package ui

import (
	"github.com/charmbracelet/bubbles/key"
	_ "github.com/mattn/go-sqlite3"
)

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Enter  key.Binding
	Back   key.Binding
	Cancel key.Binding
	Quit   key.Binding
	Help   key.Binding
	Tab    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Select, k.Enter},
		{k.Back, k.Tab, k.Quit},
	}
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
	Select: key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter/space", "select")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	Back:   key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "back")),
	Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Quit:   key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle help")),
	Tab:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch view")),
}
