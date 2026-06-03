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
            toolbar
            WebViewWrapper(webView: viewModel.webView)
        }
        .onReceive(viewModel.$currentURL) { url in
            urlText = url
        }
    }

    @ViewBuilder
    private var toolbar: some View {
        HStack(spacing: 6) {
            Button(action: { viewModel.webView.goBack() }) {
                Image(systemName: "chevron.left")
            }
            .disabled(!viewModel.canGoBack)
            .buttonStyle(.borderless)

            Button(action: { viewModel.webView.goForward() }) {
                Image(systemName: "chevron.right")
            }
            .disabled(!viewModel.canGoForward)
            .buttonStyle(.borderless)

            Button(action: { viewModel.webView.reload() }) {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)

            TextField("Enter URL", text: $urlText)
                .textFieldStyle(.roundedBorder)
                .onSubmit {
                    Task { try? await viewModel.navigate(to: urlText) }
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
}

// MARK: - WKWebView NSViewRepresentable

public struct WebViewWrapper: NSViewRepresentable {
    let webView: WKWebView

    public func makeNSView(context: Context) -> WKWebView {
        webView
    }

    public func updateNSView(_ nsView: WKWebView, context: Context) { }
}
