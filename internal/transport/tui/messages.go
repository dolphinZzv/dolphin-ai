package tui

import "strings"

// messageBuffer owns the conversation viewport's rendered output and the
// incremental-rendering bookkeeping that keeps large conversations cheap to
// append to.
//
// The buffer is intentionally pure: it knows nothing about the bubbletea
// viewport, the selection state, or the model. Mutating methods update only
// buffer fields (messages, renderedContent, blockOffsets);
// the caller is responsible for pushing renderedContent to the viewport via
// model.syncViewport after a mutation. This separation keeps the rendering
// engine testable without a live viewport.
//
// Block model — the buffer groups consecutive "text" entries into a single
// rendered block (one markdown render covers the whole run); every other
// style is its own block. blockOffsets[i] is the byte offset in
// renderedContent where block i starts, so an append that only changes the
// tail can truncate at a block boundary and re-render just the suffix instead
// of rebuilding the whole document.
type messageBuffer struct {
	messages        []renderEntry
	renderedContent string
	blockOffsets    []int // byte offset in renderedContent where each block starts
}

// append merges a new entry into the buffer and updates renderedContent.
// Consecutive "text" entries are merged into a single text run so streaming
// deltas render as one paragraph rather than one line per token.
//
// Every delta — including a merge into an existing text run — re-renders the
// current text block through glamour immediately. This keeps the viewport
// showing formatted markdown at all times during streaming, instead of raw
// markdown source that only gets rendered at the end of the turn (which
// caused a jarring "raw text, then re-rendered" flicker). The incremental
// engine re-renders only the tail text block, so the per-delta cost stays
// bounded by the size of the current paragraph, not the whole document.
func (b *messageBuffer) append(e renderEntry) {
	if e.style == "text" {
		lines := strings.Split(e.content, "\n")
		for i, line := range lines {
			if i > 0 {
				b.messages = append(b.messages, renderEntry{content: line, style: "text"})
			} else {
				n := len(b.messages)
				if n > 0 && b.messages[n-1].style == "text" {
					b.messages[n-1].content += line
				} else {
					b.messages = append(b.messages, renderEntry{content: line, style: "text"})
				}
			}
		}
		b.renderIncremental()
	} else {
		b.messages = append(b.messages, e)
		b.renderIncremental()
	}
	if len(b.messages) > maxMessages {
		b.trimFront()
	}
}

// trimFront drops the oldest half of the buffer when it exceeds maxMessages,
// then fully rebuilds. Dropping in block-aligned chunks keeps blockOffsets
// consistent with the truncated message list.
func (b *messageBuffer) trimFront() {
	keep := maxMessages / 2
	if keep <= 0 {
		return
	}
	dropBlocks := b.messageBlockIndex(len(b.messages) - keep)
	if dropBlocks <= 0 {
		dropBlocks = 1
	}
	msgStart := b.blockMessageStart(dropBlocks)
	b.messages = b.messages[msgStart:]
	b.fullRebuild()
}

// textRunStart scans backwards from idx to the first entry that is not "text",
// returning the start index of the text run containing idx.
func (b *messageBuffer) textRunStart(idx int) int {
	for idx > 0 && b.messages[idx-1].style == "text" {
		idx--
	}
	return idx
}

// messageBlockIndex returns the block index containing the given message
// index. A block is either a single non-text entry or a maximal run of text
// entries.
func (b *messageBuffer) messageBlockIndex(msgIdx int) int {
	blk := 0
	i := 0
	for i <= msgIdx && i < len(b.messages) {
		if b.messages[i].style == "text" {
			for i < len(b.messages) && b.messages[i].style == "text" {
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

// renderIncremental re-renders only the tail of renderedContent that changed,
// truncating at the affected block boundary and appending a freshly rendered
// suffix. Falls back to fullRebuild when the change touches the first block
// or when no previous offsets exist.
func (b *messageBuffer) renderIncremental() {
	if len(b.messages) == 0 {
		b.renderedContent = ""
		b.blockOffsets = nil
		return
	}

	lastIdx := len(b.messages) - 1
	reRenderFrom := lastIdx
	if b.messages[lastIdx].style == "text" {
		reRenderFrom = b.textRunStart(lastIdx)
	}

	oldBlockCount := len(b.blockOffsets)
	if oldBlockCount == 0 {
		b.fullRebuild()
		return
	}

	truncateBlock := b.messageBlockIndex(reRenderFrom)
	if truncateBlock >= oldBlockCount {
		truncateBlock = oldBlockCount
	}

	newBlockCount := b.countBlocks()
	if truncateBlock >= newBlockCount {
		return
	}

	if truncateBlock == 0 {
		b.fullRebuild()
		return
	}

	if truncateBlock < len(b.blockOffsets) {
		b.renderedContent = b.renderedContent[:b.blockOffsets[truncateBlock]]
		b.blockOffsets = b.blockOffsets[:truncateBlock]
	}

	msgStart := b.blockMessageStart(truncateBlock)
	tail := b.renderBlocks(msgStart)

	if len(b.renderedContent) > 0 && len(tail) > 0 && b.renderedContent[len(b.renderedContent)-1] != '\n' {
		b.renderedContent += "\n"
	}
	offset := len(b.renderedContent)
	b.renderedContent += tail

	b.blockOffsets = append(b.blockOffsets, b.computeBlockOffsets(msgStart, offset)...)
}

// countBlocks returns the total number of rendered blocks in the current
// message list.
func (b *messageBuffer) countBlocks() int {
	n := 0
	i := 0
	for i < len(b.messages) {
		if b.messages[i].style == "text" {
			for i < len(b.messages) && b.messages[i].style == "text" {
				i++
			}
		} else {
			i++
		}
		n++
	}
	return n
}

// blockMessageStart returns the message index where block blk begins.
func (b *messageBuffer) blockMessageStart(blk int) int {
	if blk <= 0 {
		return 0
	}
	i := 0
	n := 0
	for i < len(b.messages) && n < blk {
		if b.messages[i].style == "text" {
			for i < len(b.messages) && b.messages[i].style == "text" {
				i++
			}
		} else {
			i++
		}
		n++
	}
	return i
}

// renderBlocks renders the message slice from startIdx onward into a single
// string, one block per line. Text runs are concatenated and passed through
// the markdown renderer; other entries are styled individually.
func (b *messageBuffer) renderBlocks(startIdx int) string {
	var out strings.Builder
	for i := startIdx; i < len(b.messages); {
		entry := b.messages[i]
		if entry.style == "text" {
			var buf strings.Builder
			for i < len(b.messages) && b.messages[i].style == "text" {
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				buf.WriteString(b.messages[i].content)
				i++
			}
			out.WriteString("\n" + renderMarkdown(buf.String()))
		} else {
			out.WriteString(renderStyled(entry))
			i++
		}
		out.WriteString("\n")
	}
	return out.String()
}

// computeBlockOffsets returns the byte offset of each block beginning at
// startIdx, given that the first block starts at baseOffset in
// renderedContent. Used to extend blockOffsets after an incremental tail
// re-render.
func (b *messageBuffer) computeBlockOffsets(startIdx int, baseOffset int) []int {
	var offsets []int
	offset := baseOffset
	i := startIdx
	for i < len(b.messages) {
		offsets = append(offsets, offset)
		if b.messages[i].style == "text" {
			var buf strings.Builder
			for i < len(b.messages) && b.messages[i].style == "text" {
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				buf.WriteString(b.messages[i].content)
				i++
			}
			block := renderMarkdown(buf.String()) + "\n"
			offset += len(block)
		} else {
			block := renderStyled(b.messages[i]) + "\n"
			offset += len(block)
			i++
		}
	}
	return offsets
}

// fullRebuild re-renders the entire message list from scratch and rebuilds
// blockOffsets. Used when the change is too close to the start for an
// incremental update, or after a trim.
func (b *messageBuffer) fullRebuild() {
	var out strings.Builder
	b.blockOffsets = nil
	i := 0
	for i < len(b.messages) {
		b.blockOffsets = append(b.blockOffsets, out.Len())
		entry := b.messages[i]
		if entry.style == "text" {
			var buf strings.Builder
			for i < len(b.messages) && b.messages[i].style == "text" {
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				buf.WriteString(b.messages[i].content)
				i++
			}
			out.WriteString("\n" + renderMarkdown(buf.String()))
		} else {
			out.WriteString(renderStyled(entry))
			i++
		}
		out.WriteString("\n")
	}
	b.renderedContent = out.String()
}
