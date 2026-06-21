package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dolphin/internal/agentio"
	"dolphin/internal/types"
)

const maxMessages = 500

// spinnerFrames is the braille spinner shown in the status bar while a turn
// is in progress, so the user gets live feedback even when tool/thinking
// output is hidden.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Message types sent via tea.Program.Send.
type (
	contentMsg     struct{ text string }
	thinkingMsg    struct{ text string }
	toolCallMsg    struct{ call types.ToolCall }
	toolResultMsg  struct{ result types.ToolResult }
	flushMsg       struct{}
	queueTickMsg   struct{}
	setAgentIOMsg  struct{ a *agentio.AgentIO }
	permRequestMsg struct {
		prompt string
		ch     chan string // response channel; the model replies here on resolve
	}
)
type (
	userSubmitMsg     struct{ text string }
	prioritySubmitMsg struct{ text string }
	modelChangeMsg    struct{ name string }
	sessionMsg        struct{ id string }
	usageMsg          struct {
		inputTokens   int
		outputTokens  int
		rounds        int
		hardReqs      int64
		reqs          int64
		hardTokens    int64
		tokens        int64
		toolCalls     int
		compMaxTokens int64
	}
)

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
	viewport           viewport.Model
	textarea           textarea.Model
	messages           []renderEntry
	permDialog         *permDialog
	width              int
	height             int
	ready              bool
	thinking           string
	inThinking         bool
	msgChan            chan string
	permCh             chan string
		inputHistory       []string
		historyPos         int
		historyDraft       string
		completions       []string
		completionIdx     int
		completionPrefix  string
		getCompletions    func(prefix string) []string
	username           string
	agentName          string
	version            string
	modelName          string
	newReply           bool
	closeBlock         bool
	showTools          bool
	showThinking       bool
	showSideStatus     bool
	workmode           string
	poolSize           int
	toolParallelism    int
	temperature        float64
	reasoningEffort    string
	reasoningEffortFor func(modelName string) string
	thinkingEnabled    bool
	thinkingFor        func(modelName string) bool
	tempFor            func(modelName string) float64
	sessionID          string
	inputTokens        int
	outputTokens       int
	rounds             int
	compMaxTokens      int64
	hardReqs           int64
	reqs               int64
	hardTokens         int64
	tokens             int64
	toolCalls          int
	setPriority        func()
	savePrefs          func()
	currentMsg         string // user message currently being processed
	msgStatus          string // "pending", "success", "error"
	msgStartedAt       time.Time
	spinFrame          int // rotating spinner frame, advanced each tick while pending
	agentIO            *agentio.AgentIO
	completedItems     []completedItem // recently finished turns

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
		textarea:       ta,
		viewport:       vp,
		showTools:      false,
		showThinking:   false,
		showSideStatus: true,
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
		// ctrl+c always force-quits, even while a permission dialog is open.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// The permission dialog is modal: it captures every keystroke and
		// returns early so nothing is typed into the textarea or routed to
		// the viewport. Only ctrl+c (above) escapes it.
		if m.permDialog != nil {
			return m.handlePermKey(msg)
		}
		// Any non-tab key clears the completions popup.
		if msg.String() != "tab" {
			m.completions = nil
			m.completionIdx = 0
			m.completionPrefix = ""
		}

		switch msg.String() {
		case "alt+enter":
			ta, _ := m.textarea.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune{'\n'},
			})
			m.textarea = ta

		case "ctrl+p":
			input := strings.TrimSpace(m.textarea.Value())
			cmds = append(cmds, func() tea.Msg { return prioritySubmitMsg{text: input} })
			m.textarea.Reset()
			m.textarea.SetHeight(1)
			return m, tea.Batch(cmds...)

		case "ctrl+g":
			m.viewport.GotoBottom()
			m.updateViewportHeight()
			return m, textarea.Blink

		case "tab":
			// Slash-command autocomplete. First tab gathers completions
			// and applies the first match; subsequent tabs cycle.
			if m.getCompletions == nil {
				return m, tea.Batch(cmds...)
			}
			input := m.textarea.Value()
			if !strings.HasPrefix(input, "/") {
				return m, tea.Batch(cmds...)
			}
			if len(m.completions) == 0 {
				m.completions = m.getCompletions(input)
				if len(m.completions) == 0 {
					return m, tea.Batch(cmds...)
				}
				m.completionPrefix = input
				m.textarea.SetValue(m.completions[0])
				m.textarea.CursorEnd()
				m.completionIdx = 0
				return m, tea.Batch(cmds...)
			}
			m.completionIdx = (m.completionIdx + 1) % len(m.completions)
			m.textarea.SetValue(m.completions[m.completionIdx])
			m.textarea.CursorEnd()
			return m, tea.Batch(cmds...)

		case "enter":
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
			if input == "/windows" || input == "/windows status" {
				m.showSideStatus = !m.showSideStatus
				m.appendEntry(renderEntry{content: fmt.Sprintf("Side panel: %s", onOff(m.showSideStatus)), style: "system"})
				m.notifyPrefsChanged()
				return m, tea.Batch(cmds...)
			}
			if input != "" {
				if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
					m.inputHistory = append(m.inputHistory, input)
					if len(m.inputHistory) > 100 {
						m.inputHistory = m.inputHistory[len(m.inputHistory)-100:]
					}
				}
				m.historyPos = -1
				cmds = append(cmds, func() tea.Msg { return userSubmitMsg{text: input} })
			}
			return m, tea.Batch(cmds...)
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
		// Tool errors are always surfaced — even when showTools is off —
		// so a failed tool call is never silently invisible. Non-error
		// results stay gated behind the showTools toggle.
		if msg.result.IsError {
			m.msgStatus = "error"
			m.appendEntry(renderEntry{
				content: fmt.Sprintf("❌ %s", strings.TrimRight(msg.result.Content, "\n")),
				style:   "tool_error",
			})
			m.viewport.GotoBottom()
			break
		}
		if !m.showTools {
			break
		}
		m.appendEntry(renderEntry{
			content: strings.TrimRight(msg.result.Content, "\n"),
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
		// Advance the working spinner each tick so the user sees live
		// feedback (spinner + elapsed) while a turn is in progress.
		if m.msgStatus == "pending" {
			m.spinFrame++
		}
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
		m.compMaxTokens = msg.compMaxTokens

	case modelChangeMsg:
		m.modelName = msg.name
		if m.tempFor != nil {
			if t := m.tempFor(msg.name); t > 0 {
				m.temperature = t
			}
		}
		if m.reasoningEffortFor != nil {
			m.reasoningEffort = m.reasoningEffortFor(msg.name)
		}
		if m.thinkingFor != nil {
			m.thinkingEnabled = m.thinkingFor(msg.name)
		}
		m.rebuildViewport()

	case permRequestMsg:
		m.permDialog = &permDialog{
			prompt:  msg.prompt,
			choices: []string{"y (once)", "a (always)", "n (deny)"},
			active:  0,
		}
		// Capture this request's response channel so the model can reply
		// when the user resolves the modal. (Each RequestPermission call
		// creates a fresh channel; the model must not hold a stale one.)
		m.permCh = msg.ch

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
	// History navigation via up/down arrow.
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up":
			if len(m.inputHistory) > 0 {
				if m.historyPos == -1 {
					m.historyDraft = m.textarea.Value()
					m.historyPos = len(m.inputHistory) - 1
				} else if m.historyPos > 0 {
					m.historyPos--
				}
				m.textarea.SetValue(m.inputHistory[m.historyPos])
				m.textarea.CursorEnd()
			}
			return m, tea.Batch(append(cmds, textarea.Blink)...)
		case "down":
			if m.historyPos >= 0 {
				m.historyPos++
				if m.historyPos >= len(m.inputHistory) {
					m.historyPos = -1
					m.textarea.SetValue(m.historyDraft)
					m.historyDraft = ""
				} else {
					m.textarea.SetValue(m.inputHistory[m.historyPos])
				}
				m.textarea.CursorEnd()
			}
			return m, tea.Batch(append(cmds, textarea.Blink)...)
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

// permChoiceMap maps the dialog's choice index to the response string the
// transport expects: 0 = once, 1 = always, 2 = deny.
var permChoiceMap = []string{"once", "always", "deny"}

// handlePermKey processes a keystroke while the permission dialog is open.
// The dialog is modal: every key is captured here and the method returns
// early, so keys never reach the textarea (no stray typing) or the viewport.
// ctrl+c is handled before this in Update and force-quits.
func (m model) handlePermKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.permDialog == nil {
		return m, nil
	}
	choices := m.permDialog.choices

	// 'a' (always) requires double-press confirmation.
	confirmOrResolve := func(idx int) (tea.Model, tea.Cmd) {
		if m.permDialog.confirmIdx != idx {
			m.permDialog.confirmIdx = idx
			m.permDialog.active = idx
			return m, nil
		}
		return m.resolvePerm(idx)
	}

	key := msg.String()
	switch key {
	case "y", "Y":
		m.permDialog.confirmIdx = -1
		return m.resolvePerm(0) // once, immediate
	case "a", "A":
		return confirmOrResolve(1) // always, needs confirm
	case "n", "N", "esc":
		m.permDialog.confirmIdx = -1
		return m.resolvePerm(2) // deny, immediate
	case "left", "h":
		m.permDialog.confirmIdx = -1
		m.permDialog.active = (m.permDialog.active - 1 + len(choices)) % len(choices)
		return m, nil
	case "right", "l":
		m.permDialog.confirmIdx = -1
		m.permDialog.active = (m.permDialog.active + 1) % len(choices)
		return m, nil
	case "enter", " ":
		m.permDialog.confirmIdx = -1
		idx := m.permDialog.active
		if idx < 0 || idx >= len(permChoiceMap) {
			idx = 2 // deny
		}
		return m.resolvePerm(idx)
	}
	// Any other key clears confirmation and is swallowed.
	m.permDialog.confirmIdx = -1
	return m, nil
}

// resolvePerm closes the dialog and emits the chosen permission response.
func (m model) resolvePerm(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(permChoiceMap) {
		idx = 2
	}
	choice := permChoiceMap[idx]
	m.permDialog = nil
	return m, func() tea.Msg { return permResponseMsg{choice: choice} }
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
	// Queue area: header + body lines (capped per category) + separator
	// above the queue. queueBodyLines matches renderQueue exactly.
	active, pending := queueCounts(m.agentIO)
	body := queueBodyLines(active, pending, len(m.completedItems))
	if body > 0 {
		fixed += body + 2 // +1 header, +1 separator
	}
	// Floating bar — the current-message bar and/or a scroll indicator —
	// shown whenever the viewport is scrolled away from the bottom.
	if !m.viewport.AtBottom() {
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
	if sw == 0 || !m.showSideStatus {
		return m.width
	}
	w := m.width - sw - 1 // 1 col gap between viewport and side panel
	if w < 10 {
		w = 10
	}
	return w
}

// queueCounts returns the active and pending populations of the agent IO,
// or zeros when no agent IO is attached.
func queueCounts(aio *agentio.AgentIO) (active, pending int) {
	if aio == nil {
		return 0, 0
	}
	a := aio.ActiveSnapshot()
	p, _, _ := aio.QueueSnapshot()
	return len(a), len(p)
}

// Per-category display caps for the queue area. Each category shows its
// most relevant slice and a "+N more" indicator when truncated, instead of
// silently dropping items.
const (
	queueMaxActive    = 5 // running agents — show all up to this
	queueMaxPending   = 3 // show the head (what runs next)
	queueMaxCompleted = 3 // show the tail (most recent)
)

// queueBodyLines returns the number of item/indicator lines the queue
// renderer will emit (excluding the "📋 Queue" header) for the given
// populations. updateViewportHeight uses this so the reserved height
// matches renderQueue exactly — no overflow, no clipping.
func queueBodyLines(active, pending, completed int) int {
	if active+pending+completed == 0 {
		return 0
	}
	n := 0
	if active > queueMaxActive {
		n += queueMaxActive + 1
	} else {
		n += active
	}
	pShown := min(pending, queueMaxPending)
	n += pShown
	if pending > pShown {
		n++ // "+N queued"
	}
	cShown := min(completed, queueMaxCompleted)
	n += cShown
	if completed > cShown {
		n++ // "+N done"
	}
	return n
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

	moreStyle := lipgloss.NewStyle().
		Foreground(adaptiveFaint).
		Italic(true)
	moreLine := func(text string) string {
		return "  " + moreStyle.Render(text)
	}

	// Active (running) agents — show all up to the cap.
	activeIDs := sortedKeys(active)
	aShown := activeIDs
	if len(aShown) > queueMaxActive {
		aShown = aShown[:queueMaxActive]
	}
	for _, id := range aShown {
		t := active[id]
		icon := styleQueueActive.Render("▶")
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		timeStr := elapsed.String()
		if t.CurrentActivity != "" {
			timeStr += " " + t.CurrentActivity
		}
		timeStr = styleQueueTime.Render(timeStr)
		lines = append(lines, renderLine(icon, t.Input, timeStr))
	}
	if len(activeIDs) > len(aShown) {
		lines = append(lines, moreLine(fmt.Sprintf("… +%d more active", len(activeIDs)-len(aShown))))
	}

	// Pending — show the head (what runs next) up to the cap, then a
	// "+N queued" indicator for the rest. Earlier the tail was shown and
	// early queue items were invisible.
	pShown := pending
	if len(pShown) > queueMaxPending {
		pShown = pShown[:queueMaxPending]
	}
	for i, t := range pShown {
		icon := styleQueueWait.Render(fmt.Sprintf("#%d", i+1))
		wait := time.Since(t.EnqueuedAt).Round(time.Second)
		timeStr := styleQueueTime.Render(wait.String())
		lines = append(lines, renderLine(icon, t.Input, timeStr))
	}
	if len(pending) > len(pShown) {
		lines = append(lines, moreLine(fmt.Sprintf("… +%d queued", len(pending)-len(pShown))))
	}

	// Recently completed — show the tail (most recent) up to the cap.
	cShown := completed
	if len(cShown) > queueMaxCompleted {
		cShown = cShown[len(cShown)-queueMaxCompleted:]
	}
	for _, c := range cShown {
		icon := styleQueueWait.Render("✓")
		timeStr := styleQueueTime.Render(c.ago + " " + c.duration.String())
		lines = append(lines, renderLine(icon, c.input, timeStr))
	}
	if len(completed) > len(cShown) {
		lines = append(lines, moreLine(fmt.Sprintf("… +%d done", len(completed)-len(cShown))))
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
	// Left side holds identity and hints; the session id is pinned to the
	// right edge.
	var leftParts []string
	leftParts = append(leftParts, "🐬 "+m.agentName+" "+m.version)
	if idx := strings.LastIndex(m.version, "-"); idx > 0 {
		leftParts = append(leftParts, "git:"+m.version[idx+1:])
	}
	var rightParts []string
	if m.msgStatus == "pending" {
		frame := spinnerFrames[m.spinFrame%len(spinnerFrames)]
		elapsed := time.Since(m.msgStartedAt).Round(time.Second)
		rightParts = append(rightParts, lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "220"}).
			Render(frame+" "+elapsed.String()))
	}
	if m.sessionID != "" {
		rightParts = append(rightParts, "session:"+truncateSessionID(m.sessionID))
	}
	// Always show turn count in the bottom bar so it's visible even
	// when the side panel is hidden or too full.
	if m.rounds > 0 {
		leftParts = append(leftParts, fmt.Sprintf("turn:%d", m.rounds))
	}
	compTok := int64(m.inputTokens + m.outputTokens)
	if compTok > 0 {
		s := fmt.Sprintf("tok:%s", formatCount(compTok))
		if m.compMaxTokens > 0 {
			pct := float64(compTok) / float64(m.compMaxTokens) * 100
			s = fmt.Sprintf("tok:%s/%s(%.0f%%)", formatCount(compTok), formatCount(m.compMaxTokens), pct)
		}
		leftParts = append(leftParts, s)
	}
	if sideStatus == "" {
		// Narrow mode: put everything on the bottom bar.
		leftParts = append(leftParts, m.modelName)
		if m.workmode != "" && m.workmode != "default" {
			leftParts = append(leftParts, m.workmode)
		}
		if m.toolParallelism > 1 {
			leftParts = append(leftParts, fmt.Sprintf("parallel:%d", m.toolParallelism))
		}
		if m.hardReqs > 0 {
			pct := float64(m.reqs) / float64(m.hardReqs) * 100
			leftParts = append(leftParts, fmt.Sprintf("req:%s/%.1f%%", formatCount(m.reqs), pct))
		}
		if m.hardTokens > 0 {
			pct := float64(m.tokens) / float64(m.hardTokens) * 100
			leftParts = append(leftParts, fmt.Sprintf("tok:%s/%.1f%%", formatCount(m.tokens), pct))
		}
		if m.toolCalls > 0 {
			leftParts = append(leftParts, fmt.Sprintf("tools:%d", m.toolCalls))
		}
		if m.inputTokens > 0 || m.outputTokens > 0 {
			leftParts = append(leftParts, fmt.Sprintf("in:%d out:%d", m.inputTokens, m.outputTokens))
		}
		leftParts = append(leftParts, fmt.Sprintf("tools %s thinking %s", onOff(m.showTools), onOff(m.showThinking)))
		leftParts = append(leftParts, fmt.Sprintf("temp:%.1f", m.temperature))
		leftParts = append(leftParts, fmt.Sprintf("pool:%d", m.poolSize))
	}
	// Wide mode (sideStatus != ""): the model lives in the side panel,
	// so the bottom bar keeps only identity (leftParts stays empty).
	statusBar := renderStatusBar(leftParts, rightParts, m.width)

	// === Row 1: viewport + side status panel (split horizontally) ===
	viewWidth := m.viewportWidth()
	viewportView := m.viewport.View()

	var viewportElements []string
	scrolled := !m.viewport.AtBottom()
	if m.currentMsg != "" && scrolled {
		viewportElements = append(viewportElements, renderCurrentMsg(m.currentMsg, m.username, m.msgStatus, viewWidth, m.viewport.ScrollPercent()))
	} else if scrolled {
		viewportElements = append(viewportElements, renderScrollIndicator(viewWidth, m.viewport.ScrollPercent()))
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
	// Completions popup: shown below the queue when autocompleting.
	if len(m.completions) > 0 {
		compStyle := lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
			Background(lipgloss.AdaptiveColor{Light: "189", Dark: "236"}).
			Padding(0, 1)
		compSep := lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "244", Dark: "238"}).
			Render(strings.Repeat("─", m.width))
		header := lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "241", Dark: "241"}).
			Render("Tab to cycle, type to filter")
		elements = append(elements, compSep)
		// Show up to 8 completions; highlight the active one.
		start := m.completionIdx / 8 * 8
		end := start + 8
		if end > len(m.completions) {
			end = len(m.completions)
		}
		var compLines []string
		for j := start; j < end; j++ {
			line := m.completions[j]
			if j == m.completionIdx {
				line = "▸ " + line
			} else {
				line = "  " + line
			}
			compLines = append(compLines, compStyle.Render(line))
		}
		if len(compLines) > 0 {
			elements = append(elements, lipgloss.JoinVertical(lipgloss.Left, compLines...))
		}
		if len(m.completions) > 8 {
			elements = append(elements, compStyle.Render(fmt.Sprintf("  … %d total", len(m.completions))))
		}
		elements = append(elements, lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "220"}).
			Render(header))
	}
	// Full-width separator + input line.
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
		// Center the dialog horizontally: left-pad every line so the box
		// sits in the middle of the terminal rather than at the left edge.
		leftPad := (m.width - lipgloss.Width(dialog)) / 2
		if leftPad < 0 {
			leftPad = 0
		}
		pad := strings.Repeat(" ", leftPad)
		for i, dl := range dialogLines {
			idx := mid - len(dialogLines)/2 + i
			if idx >= 0 && idx < len(lines) {
				lines[idx] = pad + dl
			}
		}
		return strings.Join(lines, "\n")
	}

	return mainView
}

func renderStatusBar(leftParts, rightParts []string, width int) string {
	s := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
		Background(adaptiveStatusBg).
		Width(width).
		Padding(0, 1)

	avail := width - 2
	if avail < 10 {
		avail = 10
	}

	right := ""
	if len(rightParts) > 0 {
		right = strings.Join(rightParts, " | ")
	}
	rightW := lipgloss.Width(right)

	// Drop left parts from right to left until left+right fits on one line.
	for i := len(leftParts); i >= 1; i-- {
		left := strings.Join(leftParts[:i], " | ")
		if lipgloss.Width(left)+rightW <= avail {
			pad := avail - lipgloss.Width(left) - rightW
			if pad < 0 {
				pad = 0
			}
			return s.Render(left + strings.Repeat(" ", pad) + right)
		}
	}
	return s.Render(leftParts[0])
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

// sidePanelBorder is the rounded box border with the top and bottom edges
// removed and dashed left/right edges. The dashed borders extend down to
// the panel's full height so the right edge meets the full-width separator
// above the queue — the panel reads as "open at top and bottom" rather
// than a closed box.
var sidePanelBorder = lipgloss.Border{
	Top:         "",
	Left:        "┊",
	Right:       "┊",
	Bottom:      "",
	TopLeft:     "",
	TopRight:    "",
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
	if innerWidth == 0 || !m.showSideStatus {
		return ""
	}
	boxInnerWidth := innerWidth - 2 - 4 // border on each side + horizontal padding 2
	sep := strings.Repeat("╌", boxInnerWidth)
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
	if m.reasoningEffort != "" {
		rows = append(rows, [2]string{"reasoning", m.reasoningEffort})
	}
	if m.thinkingEnabled {
		rows = append(rows, [2]string{"thinking", "enabled"})
	}
	if m.toolParallelism > 1 {
		rows = append(rows, [2]string{"parallel", fmt.Sprintf("%d", m.toolParallelism)})
	}
	if m.workmode != "" && m.workmode != "default" {
		rows = append(rows, [2]string{"workmode", m.workmode})
	}
	if m.rounds > 0 {
		rows = append(rows, [2]string{"turn", fmt.Sprintf("%d", m.rounds)})
		if m.toolCalls > 0 {
			rows = append(rows, [2]string{"calls", fmt.Sprintf("%d", m.toolCalls)})
		}
	}
	if m.inputTokens > 0 || m.outputTokens > 0 {
		rows = append(rows, [2]string{"in/out", fmt.Sprintf("%d/%d", m.inputTokens, m.outputTokens)})
	}
	rows = append(rows, [2]string{"tools", onOff(m.showTools)})
	rows = append(rows, [2]string{"thinking", onOff(m.showThinking)})
	// Limit rows pinned to the bottom so they stay visible at the panel's
	// foot regardless of how many status rows appear above.
	if m.rounds > 0 {
		if m.hardReqs > 0 {
			pct := float64(m.reqs) / float64(m.hardReqs) * 100
			rows = append(rows, [2]string{"req", fmt.Sprintf("%s/%.1f%%", formatCount(m.reqs), pct)})
		}
		if m.hardTokens > 0 {
			pct := float64(m.tokens) / float64(m.hardTokens) * 100
			rows = append(rows, [2]string{"tok", fmt.Sprintf("%s/%.1f%%", formatCount(m.tokens), pct)})
		}
	}

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

	// lipgloss Width/Height set the *content* area; the border draws
	// outside it, adding 2 cols (left+right) and 2 rows (top+bottom) —
	// even when the border's Top/Bottom edges are empty strings. The
	// layout reserves `innerWidth` cols and `targetHeight` rows for the
	// panel's OUTER footprint, so subtract 2 on each axis. Otherwise the
	// panel overshoots by 2 in both directions: the 2-row vertical
	// overflow scrolls the top of the whole view off-screen (hiding the
	// "Status" header and the top of the message viewport).
	contentWidth := innerWidth - 2
	if contentWidth < 4 {
		contentWidth = 4
	}
	contentHeight := targetHeight - 2
	if contentHeight < 3 {
		contentHeight = 3
	}
	// Height pads short content but never truncates, so trim overflowing
	// lines ourselves to keep the box exactly targetHeight tall.
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > contentHeight {
		bodyLines = bodyLines[:contentHeight]
	}
	body = strings.Join(bodyLines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(sidePanelBorder).
		BorderForeground(lipgloss.AdaptiveColor{Light: "244", Dark: "238"}).
		Padding(0, 2).
		Width(contentWidth).
		Height(contentHeight)
	return boxStyle.Render(body)
}

func renderCurrentMsg(msg, username, status string, width int, scrollPct float64) string {
	icon := "⏳"
	switch status {
	case "success":
		icon = "✅"
	case "error":
		icon = "❌"
	}
	label := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "220"}).
		Render(icon + " " + username + ":")
	// When scrolled up, append a scroll-position + jump hint on the right
	// so the user knows how far up they are and how to return.
	scrollSuffix := ""
	if scrollPct >= 0 {
		scrollSuffix = lipgloss.NewStyle().
			Foreground(adaptiveFaint).
			Render(fmt.Sprintf("↑ %d%% · Ctrl+G", int(scrollPct*100+0.5)))
	}
	avail := width - lipgloss.Width(label) - 3 - lipgloss.Width(scrollSuffix)
	if avail < 4 {
		avail = 4
	}
	body := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
		MaxWidth(avail).
		Render(msg)
	// Pad body so the scroll suffix right-aligns.
	pad := avail - lipgloss.Width(body)
	if pad < 0 {
		pad = 0
	}
	content := label + " " + body + strings.Repeat(" ", pad)
	if scrollSuffix != "" {
		content += " " + scrollSuffix
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(adaptiveFaint).
		Padding(0, 1).
		Width(width).
		Render(content)
}

// renderScrollIndicator is the floating bar shown when the user has scrolled
// up but no message is currently being processed. It reports scroll position
// and the key to jump back to the latest output.
func renderScrollIndicator(width int, scrollPct float64) string {
	text := fmt.Sprintf("↑ scrolled to %d%% — Ctrl+G or PgDn to jump to bottom", int(scrollPct*100+0.5))
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(adaptiveFaint).
		Foreground(adaptiveFaint).
		Padding(0, 1).
		Width(width).
		Render(text)
}
