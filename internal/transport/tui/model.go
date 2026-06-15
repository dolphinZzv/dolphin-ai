package tui

import (
	"fmt"
	"strings"

	"dolphin/internal/types"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Message types sent via tea.Program.Send.
type contentMsg struct{ text string }
type thinkingMsg struct{ text string }
type toolCallMsg struct{ call types.ToolCall }
type toolResultMsg struct{ result types.ToolResult }
type flushMsg struct{}
type permRequestMsg struct{ prompt string }
type userSubmitMsg struct{ text string }
type modelChangeMsg struct{ name string }

// renderEntry is a rendered line or block in the conversation viewport.
type renderEntry struct {
	content string
	style   string // "text", "user", "thinking", "tool_call", "tool_result", "system"
}

type model struct {
	viewport     viewport.Model
	textarea     textarea.Model
	messages     []renderEntry
	permDialog   *permDialog
	width        int
	height       int
	ready        bool
	thinking     string
	inThinking   bool
	msgChan      chan string
	permCh       chan string
	username     string
	agentName    string
	modelName    string
	newReply     bool
	closeBlock   bool
	theme        Theme
	themeName    string
	showTools    bool
	showThinking bool

	// Incremental rendering state.
	renderedContent string
	blockOffsets    []int // byte offset in renderedContent where each output block starts
}

func newModel() model {
	ta := textarea.New()
	ta.Placeholder = "Message (Enter to send, Alt+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.Focus()

	vp := viewport.New(80, 20)
	vp.SetContent("")

	return model{
		textarea:     ta,
		viewport:     vp,
		showTools:    false,
		showThinking: false,
	}
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.ready = true
		}
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 5
		m.textarea.SetWidth(msg.Width - 1)
		cmds = append(cmds, tea.ClearScreen)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "alt+enter":
			// Insert newline without submitting.
			ta, _ := m.textarea.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune{'\n'},
			})
			m.textarea = ta

		case "enter":
			if m.permDialog == nil {
				input := strings.TrimSpace(m.textarea.Value())
				m.textarea.Reset()
				m.textarea.SetHeight(1)
				if input == "exit" || input == "/exit" {
					return m, tea.Quit
				}
				if input == "/theme" {
					m.appendEntry(renderEntry{content: fmt.Sprintf("Usage: /theme dark|light|auto  (current: %s)", m.currentThemeName()), style: "system"})
					return m, tea.Batch(cmds...)
				}
				if strings.HasPrefix(input, "/theme ") {
					m.switchTheme(strings.TrimSpace(strings.TrimPrefix(input, "/theme ")))
					m.appendEntry(renderEntry{content: fmt.Sprintf("Theme switched to %s", m.currentThemeName()), style: "system"})
					return m, tea.Batch(cmds...)
				}
				if input == "/tools" {
					m.showTools = !m.showTools
					m.appendEntry(renderEntry{content: fmt.Sprintf("Tool calls: %s", onOff(m.showTools)), style: "system"})
					return m, tea.Batch(cmds...)
				}
				if input == "/thinking" {
					m.showThinking = !m.showThinking
					m.appendEntry(renderEntry{content: fmt.Sprintf("Thinking: %s", onOff(m.showThinking)), style: "system"})
					return m, tea.Batch(cmds...)
				}
				if input != "" {
					cmds = append(cmds, func() tea.Msg { return userSubmitMsg{text: input} })
				}
				return m, tea.Batch(cmds...)
			}
		}

	case contentMsg:
		if m.newReply {
			if m.closeBlock {
				m.appendEntry(renderEntry{content: strings.Repeat("-", m.width), style: "separator"})
			}
			m.appendEntry(renderEntry{content: renderSeparator(" ", m.width), style: "separator"})
			m.newReply = false
			m.closeBlock = false
		}
		m.inThinking = false
		m.appendEntry(renderEntry{content: msg.text, style: "text"})
		m.closeBlock = true
		m.viewport.GotoBottom()

	case thinkingMsg:
		if !m.showThinking {
			m.viewport.GotoBottom()
			break
		}
		if m.inThinking {
			m.thinking += msg.text
			n := len(m.messages)
			if n > 0 && m.messages[n-1].style == "thinking" {
				m.messages[n-1].content = "💭 " + m.thinking
				m.rebuildViewport()
			}
		} else {
			m.inThinking = true
			m.thinking = msg.text
			m.appendEntry(renderEntry{content: "💭 " + msg.text, style: "thinking"})
		}
		m.viewport.GotoBottom()

	case toolCallMsg:
		if !m.showTools {
			break
		}
		m.appendEntry(renderEntry{
			content: fmt.Sprintf("🔧 %s(%s)", msg.call.Name, msg.call.Arguments),
			style:   "tool_call",
		})
		m.viewport.GotoBottom()

	case toolResultMsg:
		if !m.showTools {
			break
		}
		prefix := ""
		if msg.result.IsError {
			prefix = "❌ "
		}
		m.appendEntry(renderEntry{
			content: fmt.Sprintf("%s%s", prefix, strings.TrimRight(msg.result.Content, "\n")),
			style:   "tool_result",
		})
		m.viewport.GotoBottom()

	case flushMsg:
		m.viewport.GotoBottom()

	case modelChangeMsg:
		m.modelName = msg.name
		m.rebuildViewport()

	case permRequestMsg:
		m.permDialog = &permDialog{
			prompt:  msg.prompt,
			choices: []string{"y (once)", "a (always)", "n (deny)"},
			active:  0,
		}

	case userSubmitMsg:
		if m.closeBlock {
			m.appendEntry(renderEntry{content: strings.Repeat("-", m.width), style: "separator"})
		}

		m.appendEntry(renderEntry{content: msg.text, style: "user_text"})
		m.viewport.GotoBottom()
		m.newReply = true
		m.closeBlock = false
		if m.msgChan != nil {
			select {
			case m.msgChan <- msg.text:
			default:
			}
		}

	case permResponseMsg:
		if m.permCh != nil {
			select {
			case m.permCh <- msg.choice:
			default:
			}
		}
	}

	// If permission dialog is active, handle its input.
	if m.permDialog != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "y":
				cmds = append(cmds, func() tea.Msg { return permResponseMsg{choice: "once"} })
				m.permDialog = nil
			case "a":
				cmds = append(cmds, func() tea.Msg { return permResponseMsg{choice: "always"} })
				m.permDialog = nil
			case "n", "esc":
				cmds = append(cmds, func() tea.Msg { return permResponseMsg{choice: "deny"} })
				m.permDialog = nil
			}
		}
	}

	// Update components.
	ta, taCmd := m.textarea.Update(msg)
	m.textarea = ta
	cmds = append(cmds, taCmd)

	// Auto-grow textarea height with content, capped at 5 lines.
	lines := strings.Count(ta.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > 5 {
		lines = 5
	}
	m.textarea.SetHeight(lines)

	vp, vpCmd := m.viewport.Update(msg)
	m.viewport = vp
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m *model) appendEntry(e renderEntry) {
	if e.style == "text" {
		lines := strings.Split(e.content, "\n")
		for i, line := range lines {
			if i > 0 {
				m.messages = append(m.messages, renderEntry{content: line, style: "text"})
			} else {
				n := len(m.messages)
				if n > 0 && m.messages[n-1].style == "text" {
					m.messages[n-1].content += line
				} else {
					m.messages = append(m.messages, renderEntry{content: line, style: "text"})
				}
			}
		}
	} else {
		m.messages = append(m.messages, e)
	}
	m.renderIncremental()
}

// textRunStart returns the index of the first message in the consecutive text
// run that contains messages[idx]. messages[idx] must be a text-style entry.
func (m *model) textRunStart(idx int) int {
	for idx > 0 && m.messages[idx-1].style == "text" {
		idx--
	}
	return idx
}

// messageBlockIndex returns the block index that contains the given message index.
func (m *model) messageBlockIndex(msgIdx int) int {
	blk := 0
	i := 0
	for i <= msgIdx && i < len(m.messages) {
		if m.messages[i].style == "text" {
			// Consume consecutive text.
			for i < len(m.messages) && m.messages[i].style == "text" {
				i++
			}
		} else {
			i++
		}
		if i <= msgIdx {
			blk++
		}
	}
	return blk
}

// renderIncremental renders only the changed tail of messages and updates the viewport.
func (m *model) renderIncremental() {
	if len(m.messages) == 0 {
		m.renderedContent = ""
		m.blockOffsets = nil
		m.viewport.SetContent("")
		return
	}

	// Find the last text-style entry that may have been merged.
	lastIdx := len(m.messages) - 1
	reRenderFrom := lastIdx
	if m.messages[lastIdx].style == "text" {
		reRenderFrom = m.textRunStart(lastIdx)
	}

	// If we're only appending and there's no text merge, reRenderFrom stays at
	// the start of the new entries. Determine how many messages were already rendered.
	oldBlockCount := len(m.blockOffsets)
	if oldBlockCount == 0 {
		// First render — do a full build.
		m.fullRebuild()
		return
	}

	// Find the block that contains reRenderFrom.
	truncateBlock := m.messageBlockIndex(reRenderFrom)
	if truncateBlock >= oldBlockCount {
		truncateBlock = oldBlockCount
	}

	// If there are no new blocks to append and no text was merged, we're done.
	newBlockCount := m.countBlocks()
	if truncateBlock >= newBlockCount {
		return
	}

	// If we're truncating near the beginning, a full rebuild is simpler.
	if truncateBlock == 0 {
		m.fullRebuild()
		return
	}

	// Truncate renderedContent at the start of truncateBlock.
	if truncateBlock < len(m.blockOffsets) {
		m.renderedContent = m.renderedContent[:m.blockOffsets[truncateBlock]]
		m.blockOffsets = m.blockOffsets[:truncateBlock]
	}

	// Render messages from the first message of truncateBlock to end.
	msgStart := m.blockMessageStart(truncateBlock)
	tail := m.renderBlocks(msgStart)

	if len(m.renderedContent) > 0 && len(tail) > 0 && m.renderedContent[len(m.renderedContent)-1] != '\n' {
		m.renderedContent += "\n"
	}
	offset := len(m.renderedContent)
	m.renderedContent += tail

	// Record block offsets for the newly rendered blocks.
	m.blockOffsets = append(m.blockOffsets, m.computeBlockOffsets(msgStart, offset)...)

	m.viewport.SetContent(m.renderedContent)
}

func (m *model) countBlocks() int {
	n := 0
	i := 0
	for i < len(m.messages) {
		if m.messages[i].style == "text" {
			for i < len(m.messages) && m.messages[i].style == "text" {
				i++
			}
		} else {
			i++
		}
		n++
	}
	return n
}

// blockMessageStart returns the first message index for the given block.
func (m *model) blockMessageStart(blk int) int {
	if blk <= 0 {
		return 0
	}
	i := 0
	b := 0
	for i < len(m.messages) && b < blk {
		if m.messages[i].style == "text" {
			for i < len(m.messages) && m.messages[i].style == "text" {
				i++
			}
		} else {
			i++
		}
		b++
	}
	return i
}

// renderBlocks renders messages from startIdx to end, joining consecutive text entries.
func (m *model) renderBlocks(startIdx int) string {
	var b strings.Builder
	for i := startIdx; i < len(m.messages); {
		entry := m.messages[i]
		if entry.style == "text" {
			var buf strings.Builder
			for i < len(m.messages) && m.messages[i].style == "text" {
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				buf.WriteString(m.messages[i].content)
				i++
			}
			b.WriteString(renderMarkdown(buf.String()))
		} else {
			b.WriteString(renderStyled(entry))
			i++
		}
		b.WriteString("\n")
	}
	return b.String()
}

// computeBlockOffsets returns byte offsets for each block rendered from startIdx,
// relative to baseOffset.
func (m *model) computeBlockOffsets(startIdx int, baseOffset int) []int {
	var offsets []int
	offset := baseOffset
	i := startIdx
	for i < len(m.messages) {
		offsets = append(offsets, offset)
		if m.messages[i].style == "text" {
			var buf strings.Builder
			for i < len(m.messages) && m.messages[i].style == "text" {
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				buf.WriteString(m.messages[i].content)
				i++
			}
			block := renderMarkdown(buf.String()) + "\n"
			offset += len(block)
		} else {
			block := renderStyled(m.messages[i]) + "\n"
			offset += len(block)
			i++
		}
	}
	return offsets
}

// fullRebuild rebuilds the entire viewport content and tracking state.
func (m *model) fullRebuild() {
	var b strings.Builder
	m.blockOffsets = nil
	i := 0
	for i < len(m.messages) {
		m.blockOffsets = append(m.blockOffsets, b.Len())
		entry := m.messages[i]
		if entry.style == "text" {
			var buf strings.Builder
			for i < len(m.messages) && m.messages[i].style == "text" {
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				buf.WriteString(m.messages[i].content)
				i++
			}
			b.WriteString(renderMarkdown(buf.String()))
		} else {
			b.WriteString(renderStyled(entry))
			i++
		}
		b.WriteString("\n")
	}
	m.renderedContent = b.String()
	m.viewport.SetContent(m.renderedContent)
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func (m *model) currentThemeName() string { return m.themeName }

func (m *model) switchTheme(name string) {
	m.themeName = name
	m.theme = ThemeFromString(name)
	ApplyTheme(m.theme)
	m.rebuildViewport()
}

func (m *model) rebuildViewport() {
	m.fullRebuild()
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	toggles := fmt.Sprintf("tools %s thinking %s", onOff(m.showTools), onOff(m.showThinking))

	statusBar := lipgloss.NewStyle().
		Foreground(m.theme.StatusForeground).
		Background(m.theme.StatusBackground).
		Width(m.width).
		Padding(0, 1).
		Render("🐬 " + m.agentName + " | " + m.modelName + " | " + toggles + " | /exit")

	sep := styleSeparator.Render(strings.Repeat("-", m.width))

	inputLine := lipgloss.NewStyle().
		Background(m.theme.UserTextBg).
		Width(m.width).
		Render(m.textarea.View())

	viewportView := m.viewport.View()

	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		viewportView,
		sep,
		inputLine,
		sep,
		statusBar,
	)

	if m.permDialog != nil {
		dialog := renderPermDialog(*m.permDialog, m.width)
		lines := strings.Split(mainView, "\n")
		mid := len(lines) / 2
		dialogLines := strings.Split(dialog, "\n")
		for i, dl := range dialogLines {
			idx := mid - len(dialogLines)/2 + i
			if idx >= 0 && idx < len(lines) {
				lines[idx] = dl
			}
		}
		return strings.Join(lines, "\n")
	}

	return mainView
}
