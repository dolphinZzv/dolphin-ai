import SwiftUI
import WebKit

public struct ContentView: View {
    @ObservedObject var viewModel: WebViewModel
    @State private var urlText: String = ""

    public init(viewModel: WebViewModel) {
        self.viewModel = viewModel
    }

    public var body: some View {
        VStack(spacing: 0) {
            tabBar
            toolbar
            webViewArea
        }
        .background(Color(NSColor.windowBackgroundColor))
        .overlay(alignment: .bottomTrailing) {
            ModeBadge(mode: viewModel.mode)
                .padding(10)
        }
        .overlay(
            RoundedRectangle(cornerRadius: 0)
                .strokeBorder(viewModel.mode == .agent ? Color.yellow : Color.green, lineWidth: 4)
        )
        .onReceive(viewModel.$currentURL) { url in
            urlText = url
        }
    }

    // MARK: - Tab bar

    @ViewBuilder
    private var tabBar: some View {
        HStack(spacing: 2) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 2) {
                    ForEach(viewModel.tabOrder, id: \.self) { tabId in
                        if let tab = viewModel.tabs[tabId] {
                            TabBarItem(
                                tab: tab,
                                isActive: tabId == viewModel.activeTabId,
                                onClose: { viewModel.closeTab(id: tabId) },
                                onActivate: { viewModel.activateTab(id: tabId) }
                            )
                        }
                    }
                }
                .padding(.horizontal, 4)
            }

            Button(action: { viewModel.createTab() }) {
                Image(systemName: "plus")
                    .font(.system(size: 10, weight: .bold))
            }
            .buttonStyle(.borderless)
            .padding(.horizontal, 6)
        }
        .frame(height: 28)
        .background(Color(NSColor.controlBackgroundColor))
        .overlay(Divider(), alignment: .bottom)
        .allowsHitTesting(viewModel.mode == .user)
    }

    // MARK: - Toolbar

    @ViewBuilder
    private var toolbar: some View {
        HStack(spacing: 6) {
            Button(action: { viewModel.activeTab?.webView.goBack() }) {
                Image(systemName: "chevron.left")
            }
            .disabled(!viewModel.canGoBack)
            .buttonStyle(.borderless)

            Button(action: { viewModel.activeTab?.webView.goForward() }) {
                Image(systemName: "chevron.right")
            }
            .disabled(!viewModel.canGoForward)
            .buttonStyle(.borderless)

            Button(action: { viewModel.activeTab?.webView.reload() }) {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)

            TextField("Enter URL", text: $urlText)
                .textFieldStyle(.roundedBorder)
                .onSubmit {
                    guard let tab = viewModel.activeTab else { return }
                    let tabId = tab.id
                    Task { try? await viewModel.navigate(to: urlText, in: tabId) }
                }

            if viewModel.isLoading {
                ProgressView()
                    .controlSize(.small)
                    .padding(.trailing, 4)
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color(NSColor.windowBackgroundColor))
        .allowsHitTesting(viewModel.mode == .user)
    }

    // MARK: - WebView area

    @ViewBuilder
    private var webViewArea: some View {
        if let tab = viewModel.activeTab {
            WebViewWrapper(webView: tab.webView, mode: viewModel.mode)
                .id(tab.id)
        }
    }
}

// MARK: - Tab bar item

struct TabBarItem: View {
    @ObservedObject var tab: Tab
    let isActive: Bool
    let onClose: () -> Void
    let onActivate: () -> Void

    var body: some View {
        HStack(spacing: 4) {
            Text(tab.title.isEmpty ? (tab.url.isEmpty ? "New Tab" : tab.url) : tab.title)
                .lineLimit(1)
                .font(.system(size: 11))
                .foregroundColor(isActive ? .primary : .secondary)
                .frame(maxWidth: 140)

            Button(action: onClose) {
                Image(systemName: "xmark")
                    .font(.system(size: 8, weight: .medium))
            }
            .buttonStyle(.borderless)
            .opacity(isActive ? 1 : 0.5)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 3)
        .background(isActive ? Color(NSColor.selectedContentBackgroundColor).opacity(0.25) : Color.clear)
        .cornerRadius(4)
        .overlay(
            RoundedRectangle(cornerRadius: 4)
                .stroke(isActive ? Color(NSColor.selectedContentBackgroundColor).opacity(0.4) : Color.clear, lineWidth: 0.5)
        )
        .onTapGesture { onActivate() }
    }
}

// MARK: - WKWebView NSViewRepresentable

public struct WebViewWrapper: NSViewRepresentable {
    let webView: WKWebView
    let mode: BrowserMode

    public init(webView: WKWebView, mode: BrowserMode) {
        self.webView = webView
        self.mode = mode
    }

    public func makeNSView(context: Context) -> NSView {
        let container = NSView()
        container.wantsLayer = true

        webView.translatesAutoresizingMaskIntoConstraints = false
        container.addSubview(webView)
        NSLayoutConstraint.activate([
            webView.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            webView.topAnchor.constraint(equalTo: container.topAnchor),
            webView.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])

        let overlay = OverlayView()
        overlay.translatesAutoresizingMaskIntoConstraints = false
        container.addSubview(overlay)
        NSLayoutConstraint.activate([
            overlay.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            overlay.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            overlay.topAnchor.constraint(equalTo: container.topAnchor),
            overlay.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])

        context.coordinator.overlay = overlay
        context.coordinator.webView = webView
        return container
    }

    public func updateNSView(_ nsView: NSView, context: Context) {
        guard let overlay = context.coordinator.overlay, let webView = context.coordinator.webView else { return }
        overlay.isHidden = mode == .user
        if let window = nsView.window {
            if mode == .agent {
                window.makeFirstResponder(overlay)
            } else {
                window.makeFirstResponder(webView)
            }
        }
    }

    public func makeCoordinator() -> Coordinator {
        Coordinator()
    }

    public static func dismantleNSView(_ nsView: NSView, coordinator: Coordinator) {
        coordinator.overlay = nil
        coordinator.webView = nil
    }
}

public final class Coordinator {
    var overlay: OverlayView?
    var webView: WKWebView?
}

// MARK: - Mode badge

struct ModeBadge: View {
    let mode: BrowserMode
    @State private var breathing = false

    var body: some View {
        HStack(spacing: 3) {
            Circle()
                .fill(mode == .agent ? Color.yellow : Color.green)
                .frame(width: 7, height: 7)
                .scaleEffect(breathing ? 1.6 : 1.0)
                .animation(.easeInOut(duration: 2).repeatForever(autoreverses: true), value: breathing)

            Text(mode == .agent ? "Agent" : "User")
                .font(.system(size: 10, weight: .medium))
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(.ultraThinMaterial)
        .cornerRadius(4)
        .onAppear { breathing = true }
    }
}

/// Transparent overlay that swallows all mouse & keyboard events to prevent user interaction in agent mode.
final class OverlayView: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? {
        guard !isHidden else { return nil }
        return self
    }

    override var acceptsFirstResponder: Bool { !isHidden }

    // Mouse events — swallow everything
    override func mouseDown(with event: NSEvent) {}
    override func mouseUp(with event: NSEvent) {}
    override func mouseDragged(with event: NSEvent) {}
    override func mouseMoved(with event: NSEvent) {}
    override func mouseEntered(with event: NSEvent) {}
    override func mouseExited(with event: NSEvent) {}
    override func rightMouseDown(with event: NSEvent) {}
    override func rightMouseUp(with event: NSEvent) {}
    override func rightMouseDragged(with event: NSEvent) {}
    override func otherMouseDown(with event: NSEvent) {}
    override func otherMouseUp(with event: NSEvent) {}
    override func otherMouseDragged(with event: NSEvent) {}
    override func scrollWheel(with event: NSEvent) {}
    override func magnify(with event: NSEvent) {}
    override func rotate(with event: NSEvent) {}
    override func swipe(with event: NSEvent) {}

    // Keyboard events — swallow everything
    override func keyDown(with event: NSEvent) {}
    override func keyUp(with event: NSEvent) {}
    override func flagsChanged(with event: NSEvent) {}
}
