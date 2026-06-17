package tui

import (
	"fmt"
	"sort"
	"strings"

	"time"

	"dolphin/internal/agentio"
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
type queueTickMsg struct{}
type setAgentIOMsg struct{ a *agentio.AgentIO }
type permRequestMsg struct{ prompt string }
type userSubmitMsg struct{ text string }
type prioritySubmitMsg struct{ text string }
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
	toolCalls    int
}

// renderEntry is a rendered line or block in the conversation viewport.
type renderEntry struct {
	content string
	style   string // "text", "user", "thinking", "tool_call", "tool_result", "system"
}

type completedItem struct {
	input    string
	ago      string
	duration time.Duration
}

type model struct {
	viewport        viewport.Model
	textarea        textarea.Model
	messages        []renderEntry
	permDialog      *permDialog
	width           int
	height          int
	ready           bool
	thinking        string
	inThinking      bool
	msgChan         chan string
	permCh          chan string
	username        string
	agentName       string
	version         string
	modelName       string
	newReply        bool
	closeBlock      bool
	showTools       bool
	showThinking    bool
	workmode        string
	poolSize        int
	toolParallelism int
	temperature     float64
	tempFor         func(modelName string) float64
	sessionID       string
	inputTokens     int
	outputTokens    int
	rounds          int
	hardReqs        int64
	reqs            int64
	hardTokens      int64
	tokens          int64
	toolCalls       int
	setPriority     func()
	savePrefs       func()
	currentMsg      string // user message currently being processed
	msgStatus       string // "pending", "success", "error"
	msgStartedAt    time.Time
	agentIO         *agentio.AgentIO
	completedItems  []completedItem // recently finished turns

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
	return tea.Batch(textarea.Blink, queueTick)
}

func queueTick() tea.Msg {
	time.Sleep(time.Second)
	return queueTickMsg{}
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
		m.updateViewportHeight()
		m.textarea.SetWidth(m.viewportWidth() - 1)
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

		case "ctrl+p":
			if m.permDialog == nil {
				input := strings.TrimSpace(m.textarea.Value())
				cmds = append(cmds, func() tea.Msg { return prioritySubmitMsg{text: input} })
				m.textarea.Reset()
				m.textarea.SetHeight(1)
				return m, tea.Batch(cmds...)
			}

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
				m.messages[n-1].content = "💭 " + padThinkingCont(m.thinking)
				m.rebuildViewport()
			}
		} else {
			m.inThinking = true
			m.thinking = msg.text
			m.appendEntry(renderEntry{content: "💭 " + padThinkingCont(msg.text), style: "thinking"})
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
		if m.currentMsg != "" {
			m.completedItems = append(m.completedItems, completedItem{
				input:    m.currentMsg,
				ago:      time.Now().Round(time.Second).Format("15:04:05"),
				duration: time.Since(m.msgStartedAt).Round(time.Second),
			})
			if len(m.completedItems) > 10 {
				m.completedItems = m.completedItems[len(m.completedItems)-10:]
			}
		}
		m.updateViewportHeight()
		if m.textBlockDirty {
			m.renderIncremental()
			m.textBlockDirty = false
		}
		m.viewport.GotoBottom()

	case queueTickMsg:
		m.updateViewportHeight()
		cmds = append(cmds, queueTick)

	case setAgentIOMsg:
		m.agentIO = msg.a
		m.updateViewportHeight()

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
		m.toolCalls = msg.toolCalls

	case modelChangeMsg:
		m.modelName = msg.name
		if m.tempFor != nil {
			if t := m.tempFor(msg.name); t > 0 {
				m.temperature = t
			}
		}
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
		m.msgStartedAt = time.Now()
		m.closeBlock = false
		if m.msgChan != nil {
			select {
			case m.msgChan <- msg.text:
			default:
			}
		}

	case prioritySubmitMsg:
		if m.setPriority != nil {
			m.setPriority()
		}
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
	// Input height changed → viewport (and side panel) must resize so the
	// bottom border stays aligned with the queue separator.
	m.updateViewportHeight()

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

func padThinkingCont(s string) string {
	return strings.ReplaceAll(s, "\n", "\n   ")
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func (m *model) updateViewportHeight() {
	if m.height == 0 {
		return
	}
	// Keep viewport width in sync with the current main-column width
	// (terminal width minus side panel when visible).
	m.viewport.Width = m.viewportWidth()
	// Textarea grows 1-5 lines as the user types multi-line input.
	taLines := strings.Count(m.textarea.Value(), "\n") + 1
	if taLines < 1 {
		taLines = 1
	}
	if taLines > 5 {
		taLines = 5
	}
	// Fixed bottom rows: separator + textarea + separator + status bar.
	fixed := taLines + 3
	// Queue area: header + items (capped at 5), plus separator above queue
	qLines := queueLineCount(m.agentIO)
	completedLines := min(len(m.completedItems), 5)
	if qLines > 0 || completedLines > 0 {
		total := min(qLines+completedLines+1, 7) // +1 for separator, capped
		fixed += total
	}
	// Current message floating bar
	if m.currentMsg != "" && !m.viewport.AtBottom() {
		fixed++
	}
	h := m.height - fixed
	if h < 3 {
		h = 3
	}
	m.viewport.Height = h
}

// viewportWidth returns the width available to the message viewport.
// When the side status panel is visible (terminal wide enough), the
// viewport takes the remaining ~80%; otherwise it gets the full width.
func (m model) viewportWidth() int {
	sw := sideStatusWidth(m.width)
	if sw == 0 {
		return m.width
	}
	w := m.width - sw - 1 // 1 col gap between viewport and side panel
	if w < 10 {
		w = 10
	}
	return w
}

func queueLineCount(aio *agentio.AgentIO) int {
	if aio == nil {
		return 0
	}
	active := aio.ActiveSnapshot()
	pending, _, _ := aio.QueueSnapshot()
	n := len(active) + len(pending)
	if n == 0 {
		return 0
	}
	return n + 1 // header
}

func renderQueue(aio *agentio.AgentIO, completed []completedItem, width int) string {
	if aio == nil {
		return ""
	}
	active := aio.ActiveSnapshot()
	pending, _, _ := aio.QueueSnapshot()
	if len(active)+len(pending)+len(completed) == 0 {
		return ""
	}

	var lines []string
	header := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "220"}).
		Bold(true).
		Render("📋 Queue")
	lines = append(lines, header)

	maxItems := 5

	renderLine := func(icon, input, timeStr string) string {
		// 2 spaces indent + icon + space + separator " — " + time
		iconWidth := lipgloss.Width(icon)
		fixedOverhead := 2 + iconWidth + 1 + 3 + lipgloss.Width(timeStr) + 1
		inputMax := width - fixedOverhead
		if inputMax < 10 {
			inputMax = 10
		}
		input = truncateInput(input, inputMax)
		// Pad input to fill available width.
		inputPad := inputMax - lipgloss.Width(input)
		if inputPad < 0 {
			inputPad = 0
		}
		return fmt.Sprintf("  %s %s%s — %s", icon, input, strings.Repeat(" ", inputPad), timeStr)
	}

	for _, id := range sortedKeys(active) {
		t := active[id]
		icon := styleQueueActive.Render("▶")
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		timeStr := elapsed.String()
		if t.CurrentActivity != "" {
			timeStr += " " + t.CurrentActivity
		}
		timeStr = styleQueueTime.Render(timeStr)
		lines = append(lines, renderLine(icon, t.Input, timeStr))
		maxItems--
	}
	start := 0
	if len(pending) > maxItems {
		start = len(pending) - maxItems
	}
	for i := start; i < len(pending); i++ {
		t := pending[i]
		icon := styleQueueWait.Render(fmt.Sprintf("#%d", i+1))
		wait := time.Since(t.EnqueuedAt).Round(time.Second)
		timeStr := styleQueueTime.Render(wait.String())
		lines = append(lines, renderLine(icon, t.Input, timeStr))
		maxItems--
	}
	// Show recently completed items if there's room.
	cstart := 0
	if len(completed) > maxItems {
		cstart = len(completed) - maxItems
	}
	for i := cstart; i < len(completed); i++ {
		c := completed[i]
		icon := styleQueueWait.Render("✓")
		timeStr := styleQueueTime.Render(c.ago + " " + c.duration.String())
		lines = append(lines, renderLine(icon, c.input, timeStr))
	}
	return strings.Join(lines, "\n")
}

func sortedKeys(m map[string]*agentio.TurnInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncateInput(s string, n int) string {
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
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

	// Side status panel shows on the right when the terminal is wide
	// enough (~20% of width, min 16 cols). Narrow terminals fall back
	// to a fuller bottom status bar.
	sideStatus := m.renderSideStatus()

	// Bottom status bar: compact. When the side panel is visible, it
	// carries model/turn/usage, so the bar only needs identity + hints.
	var barParts []string
	barParts = append(barParts, "🐬 "+m.agentName+" "+m.version)
	if m.sessionID != "" {
		barParts = append(barParts, truncateSessionID(m.sessionID))
	}
	if sideStatus == "" {
		// Narrow mode: put everything on the bottom bar.
		barParts = append(barParts, m.modelName)
		if m.workmode != "" && m.workmode != "default" {
			barParts = append(barParts, m.workmode)
		}
		if m.toolParallelism > 1 {
			barParts = append(barParts, fmt.Sprintf("parallel:%d", m.toolParallelism))
		}
		if m.rounds > 0 {
			barParts = append(barParts, fmt.Sprintf("turn:%d", m.rounds))
			if m.hardReqs > 0 {
				pct := float64(m.reqs) / float64(m.hardReqs) * 100
				barParts = append(barParts, fmt.Sprintf("req:%s/%.1f%%", formatCount(m.reqs), pct))
			}
			if m.hardTokens > 0 {
				pct := float64(m.tokens) / float64(m.hardTokens) * 100
				barParts = append(barParts, fmt.Sprintf("tok:%s/%.1f%%", formatCount(m.tokens), pct))
			}
			if m.toolCalls > 0 {
				barParts = append(barParts, fmt.Sprintf("tools:%d", m.toolCalls))
			}
		}
		if m.inputTokens > 0 || m.outputTokens > 0 {
			barParts = append(barParts, fmt.Sprintf("in:%d out:%d", m.inputTokens, m.outputTokens))
		}
		barParts = append(barParts, fmt.Sprintf("tools %s thinking %s", onOff(m.showTools), onOff(m.showThinking)))
		barParts = append(barParts, fmt.Sprintf("temp:%.1f", m.temperature))
		barParts = append(barParts, fmt.Sprintf("pool:%d", m.poolSize))
	} else {
		barParts = append(barParts, m.modelName)
		barParts = append(barParts, "/exit")
	}
	statusBar := renderStatusBar(barParts, m.width)

	// === Row 1: viewport + side status panel (split horizontally) ===
	viewWidth := m.viewportWidth()
	viewportView := m.viewport.View()

	var viewportElements []string
	if m.currentMsg != "" && !m.viewport.AtBottom() {
		viewportElements = append(viewportElements, renderCurrentMsg(m.currentMsg, m.username, m.msgStatus, viewWidth))
	}
	viewportElements = append(viewportElements, viewportView)
	viewportColumn := lipgloss.JoinVertical(lipgloss.Left, viewportElements...)

	var topRow string
	if sideStatus != "" {
		topRow = lipgloss.JoinHorizontal(lipgloss.Top, viewportColumn, " ", sideStatus)
	} else {
		topRow = viewportColumn
	}

	// === Row 2: full-width queue ===
	fullSep := styleSeparator.Render(strings.Repeat("-", m.width))
	var elements []string
	elements = append(elements, topRow)
	if q := renderQueue(m.agentIO, m.completedItems, m.width); q != "" {
		elements = append(elements, fullSep, q)
	}

	// === Row 3..: full-width input + separator + status bar ===
	inputLine := lipgloss.NewStyle().
		Width(m.width).
		Render(m.textarea.View())
	elements = append(elements, fullSep, inputLine, fullSep, statusBar)

	mainView := lipgloss.JoinVertical(lipgloss.Left, elements...)

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

	// Drop parts from right to left until it fits on one line.
	for i := len(parts); i >= 1; i-- {
		if lipgloss.Width(strings.Join(parts[:i], " | ")) <= avail {
			return s.Render(strings.Join(parts[:i], " | "))
		}
	}
	return s.Render(parts[0])
}

// formatCount renders an integer with k/m/b/t suffixes for compact display
// in the status bar: 999 → "999", 1200 → "1.2k", 1.5m → "1.5m",
// 2_300_000_000 → "2.3b", 4_000_000_000_000 → "4.0t".
func formatCount(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n < 1_000_000_000_000:
		return fmt.Sprintf("%.1fb", float64(n)/1_000_000_000)
	default:
		return fmt.Sprintf("%.1ft", float64(n)/1_000_000_000_000)
	}
}

// sideStatusFraction is the fraction of terminal width reserved for the
// right-hand status panel (the rest goes to the message viewport).
// 0.2 = 20% right panel, 80% main column.
const sideStatusFraction = 0.2

// minSideStatusWidth is the minimum width the side panel needs to be
// readable. Below this, the panel is hidden and the viewport takes
// the full width (with all status info going to the bottom bar).
const minSideStatusWidth = 16

// sideStatusWidth returns the actual column width allocated to the
// side panel given the terminal width, or 0 when the terminal is too
// narrow.
func sideStatusWidth(termWidth int) int {
	w := int(float64(termWidth) * sideStatusFraction)
	if w < minSideStatusWidth {
		return 0
	}
	return w
}

// sideLabelWidth is the fixed label-column width inside the side panel.
// Longest label is "thinking" (8 chars); pad shorter labels to this width
// so values align in a tidy right column.
const sideLabelWidth = 9

// sidePanelBorder is the rounded box border with the bottom edge removed
// and dashed left/right edges. The dashed borders extend down to the
// panel's full height so they meet the full-width separator above the
// queue — the panel reads as "open at the bottom" rather than a closed
// box.
var sidePanelBorder = lipgloss.Border{
	Top:         "─",
	Left:        "┊",
	Right:       "┊",
	Bottom:      "",
	TopLeft:     "╭",
	TopRight:    "╮",
	BottomLeft:  "",
	BottomRight: "",
}

// renderSideStatus builds the vertical status panel shown to the right
// of the message viewport. Returns an empty string when the terminal is
// too narrow — in that case the panel is hidden and the viewport takes
// the full width.
//
// The panel's total height (borders included) is set explicitly to fill
// the viewport row, so its bottom border sits flush against the
// full-width separator above the queue.
//
// Long values are truncated to fit; the label column is fixed-width so
// values never wrap to a new line.
func (m model) renderSideStatus() string {
	innerWidth := sideStatusWidth(m.width)
	if innerWidth == 0 {
		return ""
	}
	boxInnerWidth := innerWidth - 2 // border on each side
	sep := strings.Repeat("─", boxInnerWidth)
	// Max value width: boxInner - label column - 1 space gap.
	maxValWidth := boxInnerWidth - sideLabelWidth - 1
	if maxValWidth < 4 {
		maxValWidth = 4
	}

	rows := [][2]string{
		{"model", m.modelName},
		{"temp", fmt.Sprintf("%.1f", m.temperature)},
		{"pool", fmt.Sprintf("%d", m.poolSize)},
	}
	if m.toolParallelism > 1 {
		rows = append(rows, [2]string{"parallel", fmt.Sprintf("%d", m.toolParallelism)})
	}
	if m.workmode != "" && m.workmode != "default" {
		rows = append(rows, [2]string{"workmode", m.workmode})
	}
	if m.rounds > 0 {
		rows = append(rows, [2]string{"turn", fmt.Sprintf("%d", m.rounds)})
		if m.hardReqs > 0 {
			pct := float64(m.reqs) / float64(m.hardReqs) * 100
			rows = append(rows, [2]string{"req", fmt.Sprintf("%s/%.1f%%", formatCount(m.reqs), pct)})
		}
		if m.hardTokens > 0 {
			pct := float64(m.tokens) / float64(m.hardTokens) * 100
			rows = append(rows, [2]string{"tok", fmt.Sprintf("%s/%.1f%%", formatCount(m.tokens), pct)})
		}
		if m.toolCalls > 0 {
			rows = append(rows, [2]string{"tools", fmt.Sprintf("%d", m.toolCalls)})
		}
	}
	if m.inputTokens > 0 || m.outputTokens > 0 {
		rows = append(rows, [2]string{"in/out", fmt.Sprintf("%d/%d", m.inputTokens, m.outputTokens)})
	}
	rows = append(rows, [2]string{"tools", onOff(m.showTools)})
	rows = append(rows, [2]string{"thinking", onOff(m.showThinking)})

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "241", Dark: "241"}).
		Width(sideLabelWidth)
	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
		MaxWidth(maxValWidth)

	lines := []string{
		lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
			Render("Status"),
		sep,
	}
	for _, r := range rows {
		label := labelStyle.Render(r[0])
		value := valueStyle.Render(r[1]) // MaxWidth truncates with ellipsis
		// Pad value to maxValWidth so each row has identical width and
		// the right border stays aligned.
		pad := maxValWidth - lipgloss.Width(value)
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, label+" "+value+strings.Repeat(" ", pad))
	}

	body := strings.Join(lines, "\n")

	// Total height = viewport row height (viewport.Height plus optional
	// current-message bar). lipgloss pads the box with blank lines so
	// its bottom border aligns with the separator above the queue.
	targetHeight := m.viewport.Height
	if m.currentMsg != "" && !m.viewport.AtBottom() {
		targetHeight++
	}
	if targetHeight < 4 {
		targetHeight = 4
	}

	boxStyle := lipgloss.NewStyle().
		Border(sidePanelBorder).
		BorderForeground(lipgloss.AdaptiveColor{Light: "244", Dark: "238"}).
		Width(innerWidth).
		Height(targetHeight)
	return boxStyle.Render(body)
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
