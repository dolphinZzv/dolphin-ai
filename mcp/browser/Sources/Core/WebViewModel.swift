import AppKit
import SwiftUI
import WebKit

// MARK: - Errors

public enum BrowserError: LocalizedError {
    case invalidURL
    case navigationFailed(String)
    case evaluateFailed(String)
    case screenshotFailed(String)
    case timeout(seconds: TimeInterval)
    case appNotActive
    case tabNotFound

    public var errorDescription: String? {
        switch self {
        case .invalidURL: return "invalid URL"
        case .navigationFailed(let msg): return "navigation failed: \(msg)"
        case .evaluateFailed(let msg): return "evaluate failed: \(msg)"
        case .screenshotFailed(let msg): return "screenshot failed: \(msg)"
        case .timeout(let s): return "operation timed out after \(Int(s))s"
        case .appNotActive: return "app is not active"
        case .tabNotFound: return "tab not found"
        }
    }
}

// MARK: - Tab

@MainActor
public class Tab: ObservableObject {
    public let id: String
    public let webView: WKWebView

    @Published public var url: String = ""
    @Published public var isLoading: Bool = false
    @Published public var canGoBack: Bool = false
    @Published public var canGoForward: Bool = false
    @Published public var title: String = ""

    var navContinuation: CheckedContinuation<Void, Error>?

    init(id: String, configuration: WKWebViewConfiguration) {
        self.id = id
        self.webView = WKWebView(frame: .zero, configuration: configuration)
        self.webView.allowsBackForwardNavigationGestures = true
    }

    deinit {
        // WKWebView is cleaned up when Tab is deallocated
    }
}

// MARK: - WebViewModel

@MainActor
public final class WebViewModel: NSObject, ObservableObject {
    @Published public var tabs: [String: Tab] = [:]
    @Published public var tabOrder: [String] = []
    @Published public var activeTabId: String = ""

    @Published public var currentURL: String = ""
    @Published public var isLoading: Bool = false
    @Published public var canGoBack: Bool = false
    @Published public var canGoForward: Bool = false

    public var activeTab: Tab? { tabs[activeTabId] }

    public static let defaultWindowWidth: CGFloat = 1200
    public static let defaultWindowHeight: CGFloat = 800

    @AppStorage("window_width") public var storedWindowWidth: Double = defaultWindowWidth
    @AppStorage("window_height") public var storedWindowHeight: Double = defaultWindowHeight

    public var windowWidth: CGFloat {
        get { CGFloat(storedWindowWidth) }
        set { storedWindowWidth = Double(newValue) }
    }

    public var windowHeight: CGFloat {
        get { CGFloat(storedWindowHeight) }
        set { storedWindowHeight = Double(newValue) }
    }

    private var screenshotDir: String = "screenshots"
    private let tabConfig: WKWebViewConfiguration

    override public init() {
        let config = WKWebViewConfiguration()
        let webpagePrefs = WKWebpagePreferences()
        webpagePrefs.allowsContentJavaScript = true
        config.defaultWebpagePreferences = webpagePrefs
        if #available(macOS 14.0, *) {
            config.preferences.isElementFullscreenEnabled = true
        }
        self.tabConfig = config
        super.init()
        applySavedUserAgent()
        _ = createTab()
    }

    /// Apply the stored window size to the first visible window.
    public func applyWindowSize() {
        guard let window = NSApp.windows.first(where: { $0.isVisible }) ?? NSApp.windows.first else { return }
        var frame = window.frame
        let newSize = CGSize(width: windowWidth, height: windowHeight)
        frame.origin.y += frame.height - newSize.height
        frame.size = newSize
        window.setFrame(frame, display: true, animate: true)
    }

    public func setScreenshotDir(_ dir: String) {
        screenshotDir = dir
    }

    // MARK: - Tab management

    @discardableResult
    public func createTab() -> String {
        let id = UUID().uuidString
        let tab = Tab(id: id, configuration: tabConfig)
        applySavedUserAgent(to: tab)
        tab.webView.navigationDelegate = self
        tabs[id] = tab
        tabOrder.append(id)
        activeTabId = id
        syncActiveTab()
        objectWillChange.send()
        return id
    }

    public func closeTab(id: String) {
        guard tabs.count > 1 else { return }
        guard let tab = tabs[id] else { return }
        tab.webView.navigationDelegate = nil
        tabs.removeValue(forKey: id)
        tabOrder.removeAll { $0 == id }
        if activeTabId == id {
            // Switch to an adjacent tab
            activeTabId = tabOrder.last ?? ""
        }
        syncActiveTab()
        objectWillChange.send()
    }

    public func activateTab(id: String) {
        guard tabs[id] != nil, id != activeTabId else { return }
        activeTabId = id
        syncActiveTab()
        objectWillChange.send()
    }

    private func syncActiveTab() {
        guard let tab = activeTab else {
            currentURL = ""
            isLoading = false
            canGoBack = false
            canGoForward = false
            return
        }
        currentURL = tab.url
        isLoading = tab.isLoading
        canGoBack = tab.canGoBack
        canGoForward = tab.canGoForward
    }

    private func syncTab(_ tab: Tab) {
        if tab.id == activeTabId {
            syncActiveTab()
        }
        objectWillChange.send()
    }

    // MARK: - User-Agent

    public func setUserAgent(_ ua: String?) {
        UserDefaults.standard.set(ua ?? "", forKey: "user_agent")
        for tab in tabs.values {
            tab.webView.customUserAgent = ua
        }
    }

    private func applySavedUserAgent() {
        let saved = UserDefaults.standard.string(forKey: "user_agent") ?? ""
        for tab in tabs.values {
            tab.webView.customUserAgent = saved.isEmpty ? nil : saved
        }
    }

    private func applySavedUserAgent(to tab: Tab) {
        let saved = UserDefaults.standard.string(forKey: "user_agent") ?? ""
        tab.webView.customUserAgent = saved.isEmpty ? nil : saved
    }

    // MARK: - Navigate

    public func navigate(to urlString: String, in tabId: String? = nil) async throws {
        guard let tab = tabId.flatMap({ tabs[$0] }) ?? activeTab else {
            throw BrowserError.tabNotFound
        }
        guard let url = URL(string: urlString) else {
            throw BrowserError.invalidURL
        }

        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            if tab.navContinuation != nil {
                continuation.resume(throwing: BrowserError.navigationFailed("previous navigation still in progress"))
                return
            }
            tab.navContinuation = continuation
            tab.webView.load(URLRequest(url: url))

            DispatchQueue.main.asyncAfter(deadline: .now() + 30) { [weak tab] in
                guard let tab, let cont = tab.navContinuation else { return }
                tab.navContinuation = nil
                cont.resume(throwing: BrowserError.timeout(seconds: 30))
            }
        }
    }

    // MARK: - Evaluate

    public func evaluate(script: String, in tabId: String? = nil) async throws -> String {
        guard let tab = tabId.flatMap({ tabs[$0] }) ?? activeTab else {
            throw BrowserError.tabNotFound
        }

        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
            let timeoutWork = DispatchWorkItem {
                continuation.resume(throwing: BrowserError.timeout(seconds: 10))
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 10, execute: timeoutWork)

            tab.webView.evaluateJavaScript(script) { result, error in
                timeoutWork.cancel()
                if let error = error {
                    continuation.resume(throwing: BrowserError.evaluateFailed(error.localizedDescription))
                    return
                }
                if let str = result as? String {
                    continuation.resume(returning: str)
                } else if let num = result as? NSNumber {
                    continuation.resume(returning: num.stringValue)
                } else if result == nil {
                    continuation.resume(returning: "")
                } else {
                    continuation.resume(returning: "\(result!)")
                }
            }
        }
    }

    // MARK: - Screenshot

    public func screenshot(url: String? = nil, outputDir: String? = nil, in tabId: String? = nil) async throws -> String {
        guard let tab = tabId.flatMap({ tabs[$0] }) ?? activeTab else {
            throw BrowserError.tabNotFound
        }

        if let url = url {
            guard let _url = URL(string: url) else {
                throw BrowserError.invalidURL
            }
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                if tab.navContinuation != nil {
                    continuation.resume(throwing: BrowserError.navigationFailed("previous navigation still in progress"))
                    return
                }
                tab.navContinuation = continuation
                tab.webView.load(URLRequest(url: _url))

                DispatchQueue.main.asyncAfter(deadline: .now() + 30) { [weak tab] in
                    guard let tab, let cont = tab.navContinuation else { return }
                    tab.navContinuation = nil
                    cont.resume(throwing: BrowserError.timeout(seconds: 30))
                }
            }
        }

        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
            let config = WKSnapshotConfiguration()
            config.afterScreenUpdates = true

            let timeoutWork = DispatchWorkItem {
                continuation.resume(throwing: BrowserError.timeout(seconds: 15))
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 15, execute: timeoutWork)

            tab.webView.takeSnapshot(with: config) { [self] image, error in
                timeoutWork.cancel()
                if let error = error {
                    continuation.resume(throwing: BrowserError.screenshotFailed(error.localizedDescription))
                    return
                }
                guard let image = image else {
                    continuation.resume(throwing: BrowserError.screenshotFailed("no image returned"))
                    return
                }

                do {
                    let dir = outputDir ?? screenshotDir
                    let path = try saveImage(image, to: dir)
                    continuation.resume(returning: path)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    // MARK: - Save image

    private func saveImage(_ image: NSImage, to dir: String) throws -> String {
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)

        let name = "screenshot_\(Int(Date().timeIntervalSince1970 * 1000)).png"
        let fileURL = URL(fileURLWithPath: dir).appendingPathComponent(name)

        guard let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
            throw BrowserError.screenshotFailed("failed to create CGImage")
        }

        let bitmap = NSBitmapImageRep(cgImage: cgImage)
        bitmap.size = image.size
        guard let data = bitmap.representation(using: .png, properties: [:]) else {
            throw BrowserError.screenshotFailed("failed to encode PNG")
        }
        try data.write(to: fileURL)
        return fileURL.path
    }
}

// MARK: - WKNavigationDelegate

extension WebViewModel: WKNavigationDelegate {
    public func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        guard let tab = tabs.first(where: { $0.value.webView === webView })?.value else { return }
        tab.url = webView.url?.absoluteString ?? ""
        tab.isLoading = false
        tab.canGoBack = webView.canGoBack
        tab.canGoForward = webView.canGoForward
        tab.title = webView.title ?? ""
        tab.navContinuation?.resume(returning: ())
        tab.navContinuation = nil
        syncTab(tab)
    }

    public func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        guard let tab = tabs.first(where: { $0.value.webView === webView })?.value else { return }
        tab.isLoading = false
        let nsError = error as NSError
        if nsError.code == NSURLErrorCancelled { return }
        tab.navContinuation?.resume(throwing: BrowserError.navigationFailed(error.localizedDescription))
        tab.navContinuation = nil
        syncTab(tab)
    }

    public func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        guard let tab = tabs.first(where: { $0.value.webView === webView })?.value else { return }
        tab.isLoading = false
        tab.navContinuation?.resume(throwing: BrowserError.navigationFailed(error.localizedDescription))
        tab.navContinuation = nil
        syncTab(tab)
    }

    public func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) {
        guard let tab = tabs.first(where: { $0.value.webView === webView })?.value else { return }
        tab.isLoading = true
        syncTab(tab)
    }

    public func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        // Page content started loading
    }
}
