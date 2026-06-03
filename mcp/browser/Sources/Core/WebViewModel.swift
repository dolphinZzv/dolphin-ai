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

    public var errorDescription: String? {
        switch self {
        case .invalidURL: return "invalid URL"
        case .navigationFailed(let msg): return "navigation failed: \(msg)"
        case .evaluateFailed(let msg): return "evaluate failed: \(msg)"
        case .screenshotFailed(let msg): return "screenshot failed: \(msg)"
        case .timeout(let s): return "operation timed out after \(Int(s))s"
        case .appNotActive: return "app is not active"
        }
    }
}

// MARK: - WebViewModel

@MainActor
public final class WebViewModel: NSObject, ObservableObject, WKNavigationDelegate {
    public let webView: WKWebView

    @Published public var currentURL: String = ""
    @Published public var isLoading: Bool = false
    @Published public var canGoBack: Bool = false
    @Published public var canGoForward: Bool = false

    private var navContinuation: CheckedContinuation<Void, Error>?
    private var screenshotDir: String = "screenshots"

    override public init() {
        let config = WKWebViewConfiguration()
        let webpagePrefs = WKWebpagePreferences()
        webpagePrefs.allowsContentJavaScript = true
        config.defaultWebpagePreferences = webpagePrefs
        if #available(macOS 14.0, *) {
            config.preferences.isElementFullscreenEnabled = true
        }
        webView = WKWebView(frame: .zero, configuration: config)
        super.init()
        webView.navigationDelegate = self
        webView.allowsBackForwardNavigationGestures = true
        applySavedUserAgent()
    }

    // MARK: - User-Agent

    public func setUserAgent(_ ua: String?) {
        webView.customUserAgent = ua
    }

    private func applySavedUserAgent() {
        let saved = UserDefaults.standard.string(forKey: "user_agent") ?? ""
        webView.customUserAgent = saved.isEmpty ? nil : saved
    }

    // MARK: - Window size

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

    // MARK: - Navigate

    public func navigate(to urlString: String) async throws {
        guard let url = URL(string: urlString) else {
            throw BrowserError.invalidURL
        }

        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            self.navContinuation = continuation
            self.webView.load(URLRequest(url: url))

            DispatchQueue.main.asyncAfter(deadline: .now() + 30) { [weak self] in
                guard let self, let cont = self.navContinuation else { return }
                self.navContinuation = nil
                cont.resume(throwing: BrowserError.timeout(seconds: 30))
            }
        }
    }

    // MARK: - Evaluate

    public func evaluate(script: String) async throws -> String {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
            let timeoutWork = DispatchWorkItem {
                continuation.resume(throwing: BrowserError.timeout(seconds: 10))
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 10, execute: timeoutWork)

            self.webView.evaluateJavaScript(script) { result, error in
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

    public func screenshot(url: String?) async throws -> String {
        if let url = url {
            try await navigate(to: url)
        }

        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
            let config = WKSnapshotConfiguration()
            config.afterScreenUpdates = true

            let timeoutWork = DispatchWorkItem {
                continuation.resume(throwing: BrowserError.timeout(seconds: 15))
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 15, execute: timeoutWork)

            self.webView.takeSnapshot(with: config) { image, error in
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
                    let path = try self.saveImage(image)
                    continuation.resume(returning: path)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    // MARK: - Save image

    private func saveImage(_ image: NSImage) throws -> String {
        let dir = screenshotDir
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

    // MARK: - WKNavigationDelegate

    public func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        currentURL = webView.url?.absoluteString ?? ""
        isLoading = false
        canGoBack = webView.canGoBack
        canGoForward = webView.canGoForward
        navContinuation?.resume(returning: ())
        navContinuation = nil
    }

    public func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        isLoading = false
        let nsError = error as NSError
        if nsError.code == NSURLErrorCancelled { return }
        navContinuation?.resume(throwing: BrowserError.navigationFailed(error.localizedDescription))
        navContinuation = nil
    }

    public func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        isLoading = false
        navContinuation?.resume(throwing: BrowserError.navigationFailed(error.localizedDescription))
        navContinuation = nil
    }

    public func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) {
        isLoading = true
    }

    public func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        // Page content started loading
    }
}
