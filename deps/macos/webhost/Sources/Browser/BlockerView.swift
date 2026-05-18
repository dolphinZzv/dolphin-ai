import AppKit

class BlockerView: NSView {
    override var acceptsFirstResponder: Bool { false }

    override func mouseDown(with event: NSEvent) {}
    override func mouseUp(with event: NSEvent) {}
    override func rightMouseDown(with event: NSEvent) {}
    override func keyDown(with event: NSEvent) {}
}