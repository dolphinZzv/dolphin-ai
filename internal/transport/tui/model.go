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

const maxMessages = 500

// Message types sent via tea.Program.Send.
type contentMsg struct{ text string }
type thinkingMsg struct{ text string }
type toolCallMsg struct{ call types.ToolCall }
type toolResultMsg struct{ result types.ToolResult }
type flushMsg struct{}
type permRequestMsg struct{ prompt string }
type userSubmitMsg struct{ text string }
type modelChangeMsg struct{ name string }
type sessionMsg struct{ id string }
type usageMsg struct {
	inputTokens  int
	outputTokens int
	rounds       int
	hardReqs     int64
	reqs         int64
	hardTokens   int64
	tokens       int64
}

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
	showTools    bool
	showThinking bool
	sessionID    string
	inputTokens  int
	outputTokens int
	rounds       int
	hardReqs     int64
	reqs         int64
	hardTokens   int64
	tokens       int64
	savePrefs    func()
	currentMsg   string // user message currently being processed
	msgStatus    string // "pending", "success", "error"

	// Incremental rendering state.
	renderedContent string
	blockOffsets    []int // byte offset in renderedContent where each output block starts
	textBlockDirty  bool  // true when last text block has merged content not yet markdown-rendered
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
		m.viewport.Height = msg.Height - 7
		m.textarea.SetWidth(msg.Width - 1)
		cmds = append(cmds, tea.ClearScreen)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "alt+enter":
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
				if input == "/tools" {
					m.showTools = !m.showTools
					m.appendEntry(renderEntry{content: fmt.Sprintf("Tool calls: %s", onOff(m.showTools)), style: "system"})
					m.notifyPrefsChanged()
					return m, tea.Batch(cmds...)
				}
				if input == "/thinking" {
					m.showThinking = !m.showThinking
					m.appendEntry(renderEntry{content: fmt.Sprintf("Thinking: %s", onOff(m.showThinking)), style: "system"})
					m.notifyPrefsChanged()
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
		m.msgStatus = "success"
		if m.textBlockDirty {
			m.renderIncremental()
			m.textBlockDirty = false
		}
		m.viewport.GotoBottom()

	case sessionMsg:
		m.sessionID = msg.id

		case usageMsg:
			m.inputTokens = msg.inputTokens
			m.outputTokens = msg.outputTokens
			m.rounds = msg.rounds
			m.hardReqs = msg.hardReqs
			m.reqs = msg.reqs
			m.hardTokens = msg.hardTokens
			m.tokens = msg.tokens

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
		m.currentMsg = msg.text
		m.msgStatus = "pending"
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
		hadMerge := false
		lines := strings.Split(e.content, "\n")
		for i, line := range lines {
			if i > 0 {
				m.messages = append(m.messages, renderEntry{content: line, style: "text"})
			} else {
				n := len(m.messages)
				if n > 0 && m.messages[n-1].style == "text" {
					m.messages[n-1].content += line
					hadMerge = true
				} else {
					m.messages = append(m.messages, renderEntry{content: line, style: "text"})
				}
			}
		}
		if hadMerge {
			m.renderedContent += e.content
			m.textBlockDirty = true
			m.viewport.SetContent(m.renderedContent)
		} else {
			m.renderIncremental()
		}
	} else {
		if m.textBlockDirty {
			m.renderIncremental()
			m.textBlockDirty = false
		}
		m.messages = append(m.messages, e)
		m.renderIncremental()
	}
	if len(m.messages) > maxMessages {
		m.trimFront()
	}
}

func (m *model) trimFront() {
	keep := maxMessages / 2
	if keep <= 0 {
		return
	}
	dropBlocks := m.messageBlockIndex(len(m.messages) - keep)
	if dropBlocks <= 0 {
		dropBlocks = 1
	}
	msgStart := m.blockMessageStart(dropBlocks)
	m.messages = m.messages[msgStart:]
	m.fullRebuild()
}

func (m *model) textRunStart(idx int) int {
	for idx > 0 && m.messages[idx-1].style == "text" {
		idx--
	}
	return idx
}

func (m *model) messageBlockIndex(msgIdx int) int {
	blk := 0
	i := 0
	for i <= msgIdx && i < len(m.messages) {
		if m.messages[i].style == "text" {
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

func (m *model) renderIncremental() {
	if len(m.messages) == 0 {
		m.renderedContent = ""
		m.blockOffsets = nil
		m.viewport.SetContent("")
		return
	}

	lastIdx := len(m.messages) - 1
	reRenderFrom := lastIdx
	if m.messages[lastIdx].style == "text" {
		reRenderFrom = m.textRunStart(lastIdx)
	}

	oldBlockCount := len(m.blockOffsets)
	if oldBlockCount == 0 {
		m.fullRebuild()
		return
	}

	truncateBlock := m.messageBlockIndex(reRenderFrom)
	if truncateBlock >= oldBlockCount {
		truncateBlock = oldBlockCount
	}

	newBlockCount := m.countBlocks()
	if truncateBlock >= newBlockCount {
		return
	}

	if truncateBlock == 0 {
		m.fullRebuild()
		return
	}

	if truncateBlock < len(m.blockOffsets) {
		m.renderedContent = m.renderedContent[:m.blockOffsets[truncateBlock]]
		m.blockOffsets = m.blockOffsets[:truncateBlock]
	}

	msgStart := m.blockMessageStart(truncateBlock)
	tail := m.renderBlocks(msgStart)

	if len(m.renderedContent) > 0 && len(tail) > 0 && m.renderedContent[len(m.renderedContent)-1] != '\n' {
		m.renderedContent += "\n"
	}
	offset := len(m.renderedContent)
	m.renderedContent += tail

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

func (m *model) fullRebuild() {
	m.textBlockDirty = false
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

func truncateSessionID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func (m *model) notifyPrefsChanged() {
	if m.savePrefs != nil {
		m.savePrefs()
	}
}

func (m *model) rebuildViewport() {
	m.fullRebuild()
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	toggles := fmt.Sprintf("tools %s thinking %s", onOff(m.showTools), onOff(m.showThinking))

	// Build all status parts, then fit into lines by actual width.
	var parts []string
	parts = append(parts, "🐬 "+m.agentName)
	if m.sessionID != "" {
		parts = append(parts, truncateSessionID(m.sessionID))
	}
	parts = append(parts, m.modelName)
	parts = append(parts, toggles)
	if m.inputTokens > 0 || m.outputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in:%d out:%d", m.inputTokens, m.outputTokens))
	}
	if m.rounds > 0 {
		if m.hardReqs > 0 {
			pct := float64(m.reqs) / float64(m.hardReqs) * 100
			parts = append(parts, fmt.Sprintf("req:%d/%d(%.1f%%)", m.reqs, m.hardReqs, pct))
		}
		if m.hardTokens > 0 {
			pct := float64(m.tokens) / float64(m.hardTokens) * 100
			parts = append(parts, fmt.Sprintf("tok:%d/%d(%.1f%%)", m.tokens, m.hardTokens, pct))
		}
		parts = append(parts, fmt.Sprintf("r%d", m.rounds))
	}
	parts = append(parts, "/exit")

	statusBar := renderStatusBar(parts, m.width)

	sep := styleSeparator.Render(strings.Repeat("-", m.width))

	inputLine := lipgloss.NewStyle().
		Width(m.width).
		Render(m.textarea.View())

	viewportView := m.viewport.View()

	var topElements []string
	if m.currentMsg != "" && !m.viewport.AtBottom() {
		topElements = append(topElements, renderCurrentMsg(m.currentMsg, m.username, m.msgStatus, m.width))
	}
	topElements = append(topElements, viewportView, sep)

	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		append(topElements, inputLine, sep, statusBar)...,
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

func renderStatusBar(parts []string, width int) string {
	s := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
		Background(adaptiveStatusBg).
		Width(width).
		Padding(0, 1)

	avail := width - 2
	if avail < 10 {
		avail = 10
	}

	// Try all on one line first.
	if lipgloss.Width(strings.Join(parts, " | ")) <= avail {
		return s.Render(strings.Join(parts, " | "))
	}

	// Find a split point where both lines fit.
	for k := len(parts) - 1; k >= 1; k-- {
		line1 := strings.Join(parts[:k], " | ")
		line2 := strings.Join(parts[k:], " | ")
		if lipgloss.Width(line1) <= avail && lipgloss.Width(line2) <= avail {
			return s.Render(line1) + "\n" + s.Render(line2)
		}
	}

	// Even split at half doesn't help — render with best-effort split.
	mid := len(parts) / 2
	return s.Render(strings.Join(parts[:mid], " | ")) + "\n" + s.Render(strings.Join(parts[mid:], " | "))
}

func renderCurrentMsg(msg, username, status string, width int) string {
	icon := "⏳"
	if status == "success" {
		icon = "✅"
	} else if status == "error" {
		icon = "❌"
	}
	label := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "220"}).
		Render(icon + " " + username + ":")
	body := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
		MaxWidth(width - lipgloss.Width(label) - 3).
		Render(msg)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(adaptiveFaint).
		Padding(0, 1).
		Width(width).
		Render(label + " " + body)
}
