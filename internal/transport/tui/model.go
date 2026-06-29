package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"dolphin/internal/agentio"
	"dolphin/internal/i18n"
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
	mcpCountMsg       struct{ count int }
	tipsMsg           struct {
		text     string
		duration time.Duration
	}
	clearTipsMsg struct{}
	usageMsg     struct {
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
	content    string
	style      string // "text", "user", "thinking", "tool_call", "tool_result", "system"
	toolCallID string // set on tool_call entries; tool_result uses it to find the matching call
}

type completedItem struct {
	input    string
	ago      string
	duration time.Duration
}

type model struct {
	viewport           viewport.Model
	textarea           textarea.Model
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
	completions        []string
	completionIdx      int
	completionPrefix   string
	getCompletions     func(prefix string) []string
	username           string
	agentName          string
	version            string
	modelName          string
	newReply           bool
	closeBlock         bool
	showTools          bool
	showThinking       bool
	showSideStatus     bool
		toolCallNames      map[string]string // ToolCallID -> tool name, for error surfacing
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
	mcpToolCount       int
	tipsText           string
	setPriority        func()
	savePrefs          func()
	currentMsg         string // user message currently being processed
	msgStatus          string // "pending", "success", "error"
	msgStartedAt       time.Time
	spinFrame          int // rotating spinner frame, advanced each tick while pending
	agentIO            *agentio.AgentIO
	completedItems     []completedItem // recently finished turns

	// Rendered conversation output and the incremental-rendering engine that
	// maintains it. Embedded so legacy field accesses (m.messages,
	// m.renderedContent, m.blockOffsets, m.textBlockDirty) and the buffer's
	// pure rendering methods resolve via promotion; the model wraps the few
	// entry points that also need to sync the bubbletea viewport.
	messageBuffer

	// Selection state for mouse-driven text selection (see selection.go).
	sel struct {
		active    bool
		startLine int
		startCol  int
		endLine   int
		endCol    int
	}
}

func newModel() model {
	ta := textarea.New()
	ta.Placeholder = "Message (Enter to send, Alt+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.Focus()

	// Adaptive background + text so typed input is readable in both
	// light and dark terminals. Colors defined in theme.go.
	styles := ta.Styles()
	styles.Focused.Base = styles.Focused.Base.Background(adaptiveInputBg)
	styles.Focused.Text = styles.Focused.Text.Background(adaptiveInputBg).Foreground(adaptiveInputFg)
	styles.Focused.Placeholder = styles.Focused.Placeholder.Foreground(adaptiveInputPh)
	styles.Focused.CursorLine = styles.Focused.CursorLine.Background(adaptiveInputBg)
	styles.Blurred.Base = styles.Blurred.Base.Background(adaptiveInputBg)
	styles.Blurred.Text = styles.Blurred.Text.Background(adaptiveInputBg).Foreground(adaptiveInputFg)
	styles.Blurred.CursorLine = styles.Blurred.CursorLine.Background(adaptiveInputBg)
	styles.Blurred.Placeholder = styles.Blurred.Placeholder.Foreground(adaptiveInputPh)
	styles.Cursor = textarea.CursorStyle{
		Color: adaptiveCursor,
		Shape: tea.CursorBlock,
		Blink: true,
	}
	ta.SetStyles(styles)

	viewportWidth = 80

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.Style = lipgloss.NewStyle()
	vp.SetContent("")

	m := model{
		textarea:       ta,
		viewport:       vp,
		showTools:      false,
		showThinking:   false,
		showSideStatus: true,
		toolCallNames:      make(map[string]string),
	}
		return m
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
		viewportWidth = m.viewportWidth()
		if !m.ready {
			m.ready = true
		}
		m.viewport.SetWidth(msg.Width)
		m.updateViewportHeight()
		m.textarea.SetWidth(m.viewportWidth() - 1)
		cmds = append(cmds, tea.ClearScreen)

	case tea.MouseMsg:
		// Wheel events pass through to viewport.Update at the bottom of
		// Update(). Non-wheel mouse events are handled by handleMouse.
		if m.permDialog != nil {
			break
		}
		switch msg.Mouse().Button { //nolint:exhaustive // non-wheel buttons handled by default via handleMouse.
		case tea.MouseWheelUp, tea.MouseWheelDown,
			tea.MouseWheelLeft, tea.MouseWheelRight:
			break // pass through to viewport.Update
		default:
			// Click on the side panel header (first row) toggles visibility.
			if msg.Mouse().Button == tea.MouseLeft {
				sideX := m.viewportWidth() + 1
				if msg.Mouse().X >= sideX && msg.Mouse().Y == 0 {
					m.showSideStatus = !m.showSideStatus
					m.updateViewportHeight()
					m.tipsText = fmt.Sprintf(i18n.T("tui.toggle_sidepanel"), onOff(m.showSideStatus))
					m.notifyPrefsChanged()
					break
				}
			}
			if cmd := m.handleMouse(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			m.autoScrollDuringDrag(msg)
		}

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
		// ESC pauses the running turn — the TUI equivalent of /session pause.
		// It is a resumable pause: the turn blocks at the next LLM/tool
		// boundary and continues on /session continue. CompactionStage also
		// honors Pause, so ESC takes effect even mid-compaction. Only active
		// while a turn is pending; when idle, ESC falls through so it still
		// dismisses the completions popup like any other key. The command is
		// routed through msgChan (same path as typed input) so the normal
		// command dispatcher sends signal.Pause and prints "session paused".
		if msg.String() == "esc" && m.msgStatus == "pending" {
			m.completions = nil
			m.completionIdx = 0
			m.completionPrefix = ""
			if m.msgChan != nil {
				select {
				case m.msgChan <- "/session pause":
				default:
				}
			}
			return m, nil
		}
		// Any non-tab key clears the completions popup.
		if msg.String() != "tab" {
			m.completions = nil
			m.completionIdx = 0
			m.completionPrefix = ""
		}

		switch msg.String() {
		case "alt+enter":
			ta, _ := m.textarea.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
				m.updateViewportHeight()
				m.showTools = !m.showTools
				m.tipsText = fmt.Sprintf(i18n.T("tui.toggle_tools"), onOff(m.showTools))
				m.notifyPrefsChanged()
				return m, tea.Batch(cmds...)
			}
			m.updateViewportHeight()
			if input == "/thinking" {
				m.showThinking = !m.showThinking
				m.tipsText = fmt.Sprintf(i18n.T("tui.toggle_thinking"), onOff(m.showThinking))
				m.notifyPrefsChanged()
				return m, tea.Batch(cmds...)
			m.updateViewportHeight()
			}
			if input == "/windows" || input == "/windows status" {
				m.showSideStatus = !m.showSideStatus
				m.tipsText = fmt.Sprintf(i18n.T("tui.toggle_sidepanel"), onOff(m.showSideStatus))
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
		case "ctrl+shift+c":
			return m, m.copySelection()
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
				m.messages[n-1].content = "✽ " + padThinkingCont(m.thinking)
				m.rebuildViewport()
			}
		} else {
			m.inThinking = true
			m.thinking = msg.text
			m.appendEntry(renderEntry{content: "✽ " + padThinkingCont(msg.text), style: "thinking"})
		}
		m.viewport.GotoBottom()

	case toolCallMsg:
		m.toolCallNames[msg.call.ID] = msg.call.Name
		if !m.showTools {
			break
		}
		m.appendEntry(renderEntry{
			content:    fmt.Sprintf("⏺ %s(%s)", msg.call.Name, msg.call.Arguments),
			style:      "tool_call",
			toolCallID: msg.call.ID,
		})
		m.viewport.GotoBottom()

	case toolResultMsg:
		// Tool errors are always surfaced — even when showTools is off —
		// so a failed tool call is never silently invisible. Non-error
		// results stay gated behind the showTools toggle.
		content := strings.TrimRight(msg.result.Content, "\n")
		if msg.result.IsError {
			m.msgStatus = "error"
			// Color the ⏺ icon red in the matching tool_call entry.
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].style == "tool_call" && m.messages[i].toolCallID == msg.result.ToolCallID {
					c := m.messages[i].content
					if strings.HasPrefix(c, "⏺") {
						rest := strings.TrimPrefix(c, "⏺")
						m.messages[i].content = lipgloss.NewStyle().Foreground(adaptiveToolIconError).Render("⏺") + rest
					}
					break
				}
			}
			// If showTools is off, no tool_call entry exists — create one now.
			found := false
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].style == "tool_call" && m.messages[i].toolCallID == msg.result.ToolCallID {
					found = true
					break
				}
			}
			if !found {
				name := m.toolCallNames[msg.result.ToolCallID]
				if name == "" {
					name = "tool"
				}
				entry := renderEntry{
					content:    lipgloss.NewStyle().Foreground(adaptiveToolIconError).Render("⏺ " + name + "(...)"),
					style:      "tool_call",
					toolCallID: msg.result.ToolCallID,
				}
				m.messages = append(m.messages, entry)
			}
			m.fullRebuild()
			m.appendEntry(renderEntry{
				content: fmt.Sprintf("  %s", content),
				style:   "tool_error",
			})
			m.viewport.GotoBottom()
			break
		}
		if !m.showTools {
			break
		}
		// Match result to the right tool call by ID so parallel
		// tools don't interleave: 🔧 A \n result-a / 🔧 B \n result-b
		idx := -1
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].style == "tool_call" && m.messages[i].toolCallID == msg.result.ToolCallID {
				idx = i
				break
			}
		}
		if idx >= 0 {
			truncated := strings.TrimSpace(content)
			if len(truncated) > 600 {
				truncated = truncated[:600] + "..."
			}
			// Update the \u23fa icon to deep green on success.
			callContent := m.messages[idx].content
			if strings.HasPrefix(callContent, "\u23fa") {
				callContent = lipgloss.NewStyle().Foreground(adaptiveToolIconOk).Render("\u23fa") + strings.TrimPrefix(callContent, "\u23fa")
			}
			m.messages[idx].content = callContent + "\n  " + truncated
			m.fullRebuild()
			m.syncViewport()
		} else {
			m.appendEntry(renderEntry{
				content: content,
				style:   "tool_result",
			})
		}
		m.viewport.GotoBottom()

	case flushMsg:
		m.msgStatus = "success"
		p := m.currentMsg
		m.currentMsg = ""
		if p != "" {
			m.completedItems = append(m.completedItems, completedItem{
				input:    p,
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
			m.syncViewport()
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

	case mcpCountMsg:
		m.mcpToolCount = msg.count

	case tipsMsg:
		m.tipsText = msg.text
		m.updateViewportHeight()
		d := msg.duration
		if d <= 0 {
			d = 3 * time.Second
		}
		return m, tea.Tick(d, func(time.Time) tea.Msg { return clearTipsMsg{} })

	case clearTipsMsg:
		m.tipsText = ""
		m.updateViewportHeight()

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
			choices: []string{"y (once)", "a (always)", "n (deny)", "esc (abort)"},
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
		m.updateViewportHeight()
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
		m.updateViewportHeight()
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
	// Arrow keys: in multi-line input, ↑/↓ move the cursor within the
	// textarea; in single-line input they scroll the conversation
	// viewport. Input-history navigation is on Ctrl+↑/Ctrl+↓ so it
	// never conflicts with cursor movement or viewport scrolling.
	skipViewport := false
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+up":
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
		case "ctrl+down":
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
		case "up", "down":
			// Multi-line input: let the textarea move the cursor between
			// lines, and skip the viewport update so ↑/↓ don't also scroll
			// the conversation (the viewport's default KeyMap binds them).
			if strings.Count(m.textarea.Value(), "\n") > 0 {
				skipViewport = true
				break
			}
			// Single-line: scroll the conversation viewport instead of
			// moving a cursor that has nowhere to go.
			if keyMsg.String() == "up" {
				m.viewport.ScrollUp(1)
			} else {
				m.viewport.ScrollDown(1)
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

	if !skipViewport {
		vp, vpCmd := m.viewport.Update(msg)
		m.viewport = vp
		cmds = append(cmds, vpCmd)
	}

	return m, tea.Batch(cmds...)
}

// permChoiceMap maps the dialog's choice index to the response string the
// transport expects: 0 = once, 1 = always, 2 = deny, 3 = abort.
var permChoiceMap = []string{"once", "always", "deny", "abort"}

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
	case "up", "k":
		m.permDialog.confirmIdx = -1
		if m.permDialog.scrollOffset > 0 {
			m.permDialog.scrollOffset--
		}
		return m, nil
	case "down", "j":
		m.permDialog.confirmIdx = -1
		m.permDialog.scrollOffset++
		return m, nil
	case "ctrl+u":
		m.permDialog.confirmIdx = -1
		m.permDialog.scrollOffset -= 5
		if m.permDialog.scrollOffset < 0 {
			m.permDialog.scrollOffset = 0
		}
		return m, nil
	case "ctrl+d":
		m.permDialog.confirmIdx = -1
		m.permDialog.scrollOffset += 5
		return m, nil
	case "g":
		m.permDialog.confirmIdx = -1
		m.permDialog.scrollOffset = 0
		return m, nil
	case "G":
		m.permDialog.confirmIdx = -1
		m.permDialog.scrollOffset = 1<<31 - 1 // max int, will be clamped in render
		return m, nil
	case "y", "Y":
		m.permDialog.confirmIdx = -1
		return m.resolvePerm(0) // once, immediate
	case "a", "A":
		return confirmOrResolve(1) // always, needs confirm
	case "n", "N":
		m.permDialog.confirmIdx = -1
		return m.resolvePerm(2) // deny, immediate
	case "esc":
		m.permDialog.confirmIdx = -1
		return m.resolvePerm(3) // abort, immediate
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

// appendEntry adds a rendered entry to the conversation buffer and syncs the
// viewport. It clears any active text selection first (the selection's byte
// offsets would be meaningless against the newly rendered content).
func (m *model) appendEntry(e renderEntry) {
	m.clearSelection()
	m.messageBuffer.append(e) //nolint:staticcheck // QF1008: explicit selector is clearer than promoted m.append.
	m.syncViewport()
}

// syncViewport pushes the buffer's renderedContent to the bubbletea viewport.
// Called after any buffer mutation; the buffer itself never touches the
// viewport so it stays free of tea/bubbles coupling.
func (m *model) syncViewport() {
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
	m.viewport.SetWidth(m.viewportWidth())
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
	// Tips line — shown between viewport and queue when a tip is active.
	if m.tipsText != "" {
		fixed++
	}
	// Queue area: header + body lines (capped per category) + separator
	// above the queue. queueBodyLines matches renderQueue exactly.
	active, pending := queueCounts(m.agentIO)
	body := queueBodyLines(active, pending, len(m.completedItems))
	if body > 0 {
		fixed += body + 2 // +1 header, +1 separator
	}
	// Floating bar — the current-message bar and/or a scroll indicator —
	// shown whenever the viewport is scrolled away from the bottom.
	if m.currentMsg != "" {
		fixed += 2
	}
	h := m.height - fixed
	if h < 3 {
		h = 3
	}
	m.viewport.SetHeight(h)
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

// queueMaxBodyLines caps the total queue body lines (excluding the header).
// The queue area is kept compact so the input area stays prominent.
const queueMaxBodyLines = 2

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
	return min(n, queueMaxBodyLines)
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
		Foreground(lipgloss.Color("208")).
		Bold(true).
		Render(i18n.T("tui.queue_title"))
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
		lines = append(lines, moreLine(fmt.Sprintf(i18n.T("tui.queue_more_active"), len(activeIDs)-len(aShown))))
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
		lines = append(lines, moreLine(fmt.Sprintf(i18n.T("tui.queue_more_queued"), len(pending)-len(pShown))))
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
		lines = append(lines, moreLine(fmt.Sprintf(i18n.T("tui.queue_more_done"), len(completed)-len(cShown))))
	}
	// Cap the total body lines (everything after the header) to keep the
	// queue compact so the input area stays prominent.
	if len(lines) > queueMaxBodyLines+1 {
		lines = lines[:queueMaxBodyLines+1]
		// Replace the last shown line with a compact "+N more" indicator.
		remaining := 0
		if activeRemaining := len(activeIDs) - min(len(activeIDs), queueMaxActive); activeRemaining > 0 {
			remaining += activeRemaining
		}
		if pendingRemaining := len(pending) - min(len(pending), queueMaxPending); pendingRemaining > 0 {
			remaining += pendingRemaining
		}
		if completedRemaining := len(completed) - min(len(completed), queueMaxCompleted); completedRemaining > 0 {
			remaining += completedRemaining
		}
		if remaining > 0 {
			lines[queueMaxBodyLines] = moreLine(fmt.Sprintf(i18n.T("tui.queue_more_queued"), remaining))
		}
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
	m.syncViewport()
}

func newTuiView(s string) tea.View {
	v := tea.NewView(s)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) View() tea.View {
	if !m.ready {
		return newTuiView(i18n.T("tui.initializing"))
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
		leftParts = append(leftParts, "🐬 "+m.agentName)
	var rightParts []string
	if m.msgStatus == "pending" {
		frame := spinnerFrames[m.spinFrame%len(spinnerFrames)]
		elapsed := time.Since(m.msgStartedAt).Round(time.Second)
		rightParts = append(rightParts, lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
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
		if m.mcpToolCount > 0 {
			leftParts = append(leftParts, fmt.Sprintf("mcp:%d", m.mcpToolCount))
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
	scrolled := !m.viewport.AtBottom()
	// Reserve 5 chars on the right for the scroll percentage overlay.
	if scrolled {
		viewWidth -= 5
	}
	var viewportView string
	switch { //nolint:gocritic // ifElseChain: three mixed-condition branches read better as if/else.
	case m.sel.active:
		viewportView = m.renderViewportContent()
	case m.showingWelcome():
		viewportView = m.renderWelcome()
	default:
		m.viewport.SetWidth(viewWidth)
		viewportView = m.viewport.View()
	}
	// Overlay scroll percentage at the bottom-right of the viewport.
	if scrolled && m.viewport.ScrollPercent() >= 0 {
		pct := m.viewport.ScrollPercent()
		if pct > 1 { pct = 1 }
		pctStr := lipgloss.NewStyle().
			Foreground(adaptiveFaint).
			Render(fmt.Sprintf(" %d%%", int(pct*100+0.5)))
		vpLines := strings.Split(viewportView, "\n")
		if len(vpLines) > 0 {
			vpLines[len(vpLines)-1] += pctStr
		}
		viewportView = strings.Join(vpLines, "\n")
	}

	var viewportElements []string
	if m.currentMsg != "" {
		// Always show the in-flight message bar at the top when
		// a turn is processing, so the user never loses sight of
		// what they asked.
		viewportElements = append(viewportElements, renderCurrentMsg(m.currentMsg, m.username, m.msgStatus, viewWidth, m.viewport.ScrollPercent()))
	}
	viewportElements = append(viewportElements, viewportView)
	viewportColumn := lipgloss.JoinVertical(lipgloss.Left, viewportElements...)

	var topRow string
	if sideStatus != "" {
		topRow = lipgloss.JoinHorizontal(lipgloss.Top, viewportColumn, " ", sideStatus)
	} else {
		topRow = viewportColumn
	}

	// === Row 2: tips + full-width queue ===
	fullSep := styleSeparator.Render(strings.Repeat("-", m.width))
	var elements []string
	elements = append(elements, topRow)
	// Tips banner between viewport and queue — brief notifications for
	// toggles, copy confirmations, etc.
	if m.tipsText != "" {
		tipLine := lipgloss.NewStyle().
			Foreground(adaptiveFaint).
			Width(m.width).
			Render("💡 " + m.tipsText)
		elements = append(elements, tipLine)
	}
	if q := renderQueue(m.agentIO, m.completedItems, m.width); q != "" {
		elements = append(elements, fullSep, q)
	}

	// === Row 3..: full-width input + separator + status bar ===
	// Completions popup: shown below the queue when autocompleting.
	if c := renderCompletions(m.completions, m.completionIdx, m.width); c != "" {
		elements = append(elements, c)
	}
	// Full-width separator + input line.
	inputLine := lipgloss.NewStyle().
		Width(m.width).
		Render(m.textarea.View())
	elements = append(elements, fullSep, inputLine, fullSep, statusBar)

	mainView := lipgloss.JoinVertical(lipgloss.Left, elements...)

	if m.permDialog != nil {
		dialog := renderPermDialog(*m.permDialog, m.width, m.height-2)
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
		return newTuiView(strings.Join(lines, "\n"))
	}

	return newTuiView(mainView)
}

func renderStatusBar(leftParts, rightParts []string, width int) string {
	s := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
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
	sep := strings.Repeat("-", boxInnerWidth)
	// Max value width: boxInner - label column - 1 space gap.
	maxValWidth := boxInnerWidth - sideLabelWidth - 1
	if maxValWidth < 4 {
		maxValWidth = 4
	}

	rows := [][2]string{
		{i18n.T("tui.label_model"), m.modelName},
		{i18n.T("tui.label_temp"), fmt.Sprintf("%.1f", m.temperature)},
		{i18n.T("tui.label_pool"), fmt.Sprintf("%d", m.poolSize)},
	}
	if m.reasoningEffort != "" {
		rows = append(rows, [2]string{i18n.T("tui.label_reasoning"), m.reasoningEffort})
	}
	if m.thinkingEnabled {
		rows = append(rows, [2]string{i18n.T("tui.label_thinking"), i18n.T("tui.label_enabled")})
	}
	if m.toolParallelism > 1 {
		rows = append(rows, [2]string{i18n.T("tui.label_parallel"), fmt.Sprintf("%d", m.toolParallelism)})
	}
	if m.workmode != "" && m.workmode != "default" {
		rows = append(rows, [2]string{i18n.T("tui.label_workmode"), m.workmode})
	}
	if m.rounds > 0 {
		rows = append(rows, [2]string{i18n.T("tui.label_turn"), fmt.Sprintf("%d", m.rounds)})
		if m.toolCalls > 0 {
			rows = append(rows, [2]string{i18n.T("tui.label_calls"), fmt.Sprintf("%d", m.toolCalls)})
		}
	}
	if m.mcpToolCount > 0 {
		rows = append(rows, [2]string{"mcp", fmt.Sprintf("%d", m.mcpToolCount)})
	}
	if m.inputTokens > 0 || m.outputTokens > 0 {
		rows = append(rows, [2]string{i18n.T("tui.label_inout"), fmt.Sprintf("%d/%d", m.inputTokens, m.outputTokens)})
	}
	rows = append(rows, [2]string{i18n.T("tui.label_tools"), onOff(m.showTools)})
	rows = append(rows, [2]string{i18n.T("tui.label_thinking"), onOff(m.showThinking)})
	// Limit rows pinned to the bottom so they stay visible at the panel's
	// foot regardless of how many status rows appear above.
	if m.rounds > 0 {
		if m.hardReqs > 0 {
			pct := float64(m.reqs) / float64(m.hardReqs) * 100
			rows = append(rows, [2]string{i18n.T("tui.label_req"), fmt.Sprintf("%s/%.1f%%", formatCount(m.reqs), pct)})
		}
		if m.hardTokens > 0 {
			pct := float64(m.tokens) / float64(m.hardTokens) * 100
			rows = append(rows, [2]string{i18n.T("tui.label_tok"), fmt.Sprintf("%s/%.1f%%", formatCount(m.tokens), pct)})
		}
	}

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Width(sideLabelWidth)
	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		MaxWidth(maxValWidth)

	lines := []string{
		lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Render(i18n.T("tui.status_title")),
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
	targetHeight := m.viewport.Height()
	if m.currentMsg != "" {
		targetHeight += 2
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
		BorderForeground(lipgloss.Color("238")).
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
		Foreground(lipgloss.Color("208")).
		Render(icon + " " + username + ":")
	// When scrolled up, append a scroll-position + jump hint on the right
	// so the user knows how far up they are and how to return.
	scrollSuffix := ""
	if scrollPct >= 0 {
		scrollSuffix = lipgloss.NewStyle().
			Foreground(adaptiveFaint).
			Render(fmt.Sprintf(i18n.T("tui.scroll_hint_short"), int(scrollPct*100+0.5)))
	}
	avail := width - lipgloss.Width(label) - 3 - lipgloss.Width(scrollSuffix)
	if avail < 4 {
		avail = 4
	}
	body := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
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
	text := fmt.Sprintf(i18n.T("tui.scroll_hint_full"), int(scrollPct*100+0.5))
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(adaptiveFaint).
		Foreground(adaptiveFaint).
		Padding(0, 1).
		Width(width).
		Render(text)
}
