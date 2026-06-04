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
        .padding(8)
        .background(Color(NSColor.windowBackgroundColor))
    }

    // MARK: - WebView area

    @ViewBuilder
    private var webViewArea: some View {
        if let tab = viewModel.activeTab {
            WebViewWrapper(webView: tab.webView)
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

    public func makeNSView(context: Context) -> WKWebView {
        webView
    }

    public func updateNSView(_ nsView: WKWebView, context: Context) { }
}
