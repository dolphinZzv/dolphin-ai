import AppKit
import Foundation
import WebKit
import SnapKit

class TabInfo: @unchecked Sendable {
    let id: String
    let webView: WKWebView
    var title: String
    let createdAt: Date

    init(id: String, webView: WKWebView, title: String = "") {
        self.id = id
        self.webView = webView
        self.title = title
        self.createdAt = Date()
    }
}

class WebKitSession: NSObject, Sendable {
    let id: String
    let webView: WKWebView
    var interactive: Bool = false
    private var blockerView: BlockerView?
    private let eventBuffer = EventBuffer()
    private let lock = NSLock()
    private let hostWindow: NSWindow
    private var borderViewRef: NSView?
    private var didCleanup = false
    private let urlField = NSTextField()
    private let urlContainer = ThemeAwareView()
    private let tabBarView = ThemeAwareView()
    private let tabBarHeight: CGFloat = 28
    private let tabDefaultWidth: CGFloat = 200
    private let tabMinWidth: CGFloat = 100
    private let backButton = NSButton()
    private let forwardButton = NSButton()
    private let progressIndicator = NSProgressIndicator()
    private var dialogHandlers: [String: (Bool, String?) -> Void] = [:]

    // Multi-tab support
    private var tabs: [String: TabInfo] = [:]
    private var activeTabId: String = ""
    private let initialTabId = "main"

    var activeWebView: WKWebView {
        tabs[activeTabId]?.webView ?? webView
    }

    init(id: String, viewport: Viewport) {
        self.id = id
        let config = WKWebViewConfiguration()
        config.websiteDataStore = WKWebsiteDataStore.default()
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        config.applicationNameForUserAgent = "Version/17.5 Safari/605.1.15"
        hostWindow = NSWindow(contentRect: NSRect(x: 0, y: 0, width: viewport.width, height: viewport.height),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                              backing: .buffered,
                              defer: false)
        hostWindow.titlebarAppearsTransparent = true
        hostWindow.title = ""
        hostWindow.backgroundColor = .windowBackgroundColor
        hostWindow.setFrameOrigin(NSPoint(x: 100, y: 100))

        let contentView = hostWindow.contentView!

        // URL field — borderless text field with custom rounded container
        urlField.isBordered = false
        urlField.bezelStyle = .roundedBezel
        urlField.font = NSFont.systemFont(ofSize: 14)
        urlField.placeholderString = "Search or enter URL"
        urlField.refusesFirstResponder = false
        urlField.focusRingType = .none
        urlField.drawsBackground = false
        urlField.backgroundColor = .clear
        urlField.usesSingleLineMode = true
        urlField.lineBreakMode = .byTruncatingHead

        // Rounded container for URL field
        urlContainer.wantsLayer = true
        urlContainer.layer?.cornerRadius = 4
        urlContainer.layer?.borderWidth = 0.5
        urlContainer.layer?.borderColor = NSColor.separatorColor.cgColor
        urlContainer.layer?.backgroundColor = NSColor.controlBackgroundColor.cgColor

        // Create a border view that wraps the webview (yellow=non-interactive, green=interactive)
        let borderView = NSView(frame: .zero)
        borderView.wantsLayer = true
        borderView.layer?.borderWidth = 2
        borderView.layer?.borderColor = NSColor.systemYellow.cgColor
        contentView.addSubview(borderView)

        // Create webView before super.init (let property)
        let wv = WKWebView(frame: .zero, configuration: config)
        wv.autoresizingMask = [.width, .height]
        webView = wv
        borderView.addSubview(wv)

        // Tab bar
        tabBarView.frame = .zero

        super.init()
        contentView.addSubview(tabBarView)
        contentView.addSubview(urlContainer)
        contentView.addSubview(urlField)

        urlContainer.onAppearanceChange = { [weak self] in
            guard let self = self else { return }
            self.urlContainer.effectiveAppearance.performAsCurrentDrawingAppearance {
                self.urlContainer.layer?.borderColor = NSColor.separatorColor.cgColor
                self.urlContainer.layer?.backgroundColor = NSColor.controlBackgroundColor.cgColor
            }
        }

        tabBarView.onAppearanceChange = { [weak self] in
            self?.rebuildTabBar()
        }

        // Global event monitor: block mouse/key events when non-interactive
        NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown, .leftMouseUp, .rightMouseUp, .keyDown]) { [weak self] event in
            guard let self = self, !self.interactive, event.window === self.hostWindow,
                  let bv = self.borderViewRef else { return event }
            let loc = event.locationInWindow
            let frame = bv.convert(bv.bounds, to: nil)
            if frame.contains(loc) {
                return nil
            }
            // For keyDown, only block if webView is first responder
            if event.type == .keyDown,
               let fr = event.window?.firstResponder as? NSView,
               fr.isDescendant(of: bv) {
                return nil
            }
            return event
        }

        // Back/Forward buttons
        backButton.bezelStyle = .shadowlessSquare
        backButton.isBordered = false
        backButton.target = self
        backButton.action = #selector(goBack)
        backButton.controlSize = .small
        backButton.image = NSImage(systemSymbolName: "chevron.left", accessibilityDescription: "Back")
        contentView.addSubview(backButton)

        forwardButton.bezelStyle = .shadowlessSquare
        forwardButton.isBordered = false
        forwardButton.target = self
        forwardButton.action = #selector(goForward)
        forwardButton.controlSize = .small
        forwardButton.image = NSImage(systemSymbolName: "chevron.right", accessibilityDescription: "Forward")
        contentView.addSubview(forwardButton)

        // Loading progress bar
        progressIndicator.style = .bar
        progressIndicator.isIndeterminate = false
        progressIndicator.minValue = 0
        progressIndicator.maxValue = 1.0
        progressIndicator.doubleValue = 0
        progressIndicator.isDisplayedWhenStopped = false
        progressIndicator.controlSize = .small
        contentView.addSubview(progressIndicator)

        urlField.target = self
        urlField.action = #selector(urlFieldSubmit)
        urlField.delegate = self
        wv.uiDelegate = self
        wv.navigationDelegate = self
        wv.addObserver(self, forKeyPath: #keyPath(WKWebView.title), options: .new, context: nil)
        wv.addObserver(self, forKeyPath: #keyPath(WKWebView.estimatedProgress), options: .new, context: nil)

        // Layout: tabBar at top, back/forward buttons + URL field below, progress bar, borderView fills rest
        tabBarView.snp.makeConstraints { make in
            make.top.equalTo(contentView).offset(4)
            make.leading.trailing.equalTo(contentView)
            make.height.equalTo(tabBarHeight)
        }
        backButton.snp.makeConstraints { make in
            make.centerY.equalTo(urlContainer)
            make.leading.equalTo(contentView).offset(8)
            make.width.height.equalTo(24)
        }
        forwardButton.snp.makeConstraints { make in
            make.centerY.equalTo(urlContainer)
            make.leading.equalTo(backButton.snp.trailing).offset(2)
            make.width.height.equalTo(24)
        }
        urlContainer.snp.makeConstraints { make in
            make.top.equalTo(tabBarView.snp.bottom).offset(4)
            make.leading.equalTo(forwardButton.snp.trailing).offset(4)
            make.trailing.equalTo(contentView).offset(-8)
            make.height.equalTo(26)
        }
        urlField.snp.makeConstraints { make in
            make.centerY.equalTo(urlContainer)
            make.leading.equalTo(urlContainer).offset(4)
            make.trailing.equalTo(urlContainer).offset(-4)
            make.height.equalTo(22)
        }
        progressIndicator.snp.makeConstraints { make in
            make.top.equalTo(urlContainer.snp.bottom).offset(4)
            make.leading.equalTo(contentView).offset(58)
            make.trailing.equalTo(contentView).offset(-8)
            make.height.equalTo(3)
        }
        borderView.snp.makeConstraints { make in
            make.top.equalTo(progressIndicator.snp.bottom).offset(4)
            make.leading.trailing.bottom.equalTo(contentView)
        }

        // Force layout so webView gets a proper frame right away
        contentView.layoutSubtreeIfNeeded()

        // Rebuild tab bar on window resize
        NotificationCenter.default.addObserver(forName: NSWindow.didResizeNotification, object: hostWindow, queue: .main) { [weak self] _ in
            self?.rebuildTabBar()
        }

        // Register the initial tab
        let mainTab = TabInfo(id: initialTabId, webView: wv, title: "main")
        tabs[initialTabId] = mainTab
        activeTabId = initialTabId

        borderViewRef = borderView
    }

    deinit {
        guard !didCleanup else { return }

        let hw = self.hostWindow
        let tabViews = Array(self.tabs.values).map { $0.webView }
        if Thread.isMainThread {
            for tv in tabViews {
                tv.navigationDelegate = nil
                tv.uiDelegate = nil
                tv.removeObserver(self, forKeyPath: #keyPath(WKWebView.title))
                tv.removeObserver(self, forKeyPath: #keyPath(WKWebView.estimatedProgress))
                tv.stopLoading()
                tv.removeFromSuperview()
            }
            hw.orderOut(nil)
            hw.close()
        } else {
            DispatchQueue.main.sync {
                for tv in tabViews {
                    tv.navigationDelegate = nil
                    tv.uiDelegate = nil
                    tv.removeObserver(self, forKeyPath: #keyPath(WKWebView.title))
                    tv.removeObserver(self, forKeyPath: #keyPath(WKWebView.estimatedProgress))
                    tv.stopLoading()
                    tv.removeFromSuperview()
                }
                hw.orderOut(nil)
                hw.close()
            }
        }
    }

    func showWindow() {
        hostWindow.makeKeyAndOrderFront(nil)
    }

    func cleanup() {
        dispatchPrecondition(condition: .onQueue(.main))
        guard !didCleanup else { return }
        didCleanup = true

        // Clean all tabs: nil delegates, remove observers, load about:blank
        for (_, tab) in tabs {
            tab.webView.navigationDelegate = nil
            tab.webView.uiDelegate = nil
            tab.webView.removeObserver(self, forKeyPath: #keyPath(WKWebView.title))
            tab.webView.removeObserver(self, forKeyPath: #keyPath(WKWebView.estimatedProgress))
            tab.webView.load(URLRequest(url: URL(string: "about:blank")!))
            tab.webView.removeFromSuperview()
        }
        hostWindow.orderOut(nil)
        hostWindow.close()
    }

    // MARK: - Tab management

    func createTab(configuration: WKWebViewConfiguration? = nil) -> String {
        let tabId = UUID().uuidString
        let config = configuration ?? WKWebViewConfiguration()
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        let newWV = WKWebView(frame: .zero, configuration: config)
        newWV.autoresizingMask = [.width, .height]
        newWV.uiDelegate = self
        newWV.navigationDelegate = self
        newWV.addObserver(self, forKeyPath: #keyPath(WKWebView.title), options: .new, context: nil)
        newWV.addObserver(self, forKeyPath: #keyPath(WKWebView.estimatedProgress), options: .new, context: nil)
        newWV.isHidden = true
        if let bv = borderViewRef {
            newWV.frame = bv.bounds
            bv.addSubview(newWV)
        }

        let info = TabInfo(id: tabId, webView: newWV)
        tabs[tabId] = info
        rebuildTabBar()
        return tabId
    }

    func rebuildTabBar() {
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            for sub in self.tabBarView.subviews {
                sub.removeFromSuperview()
            }

            let tabIds = Array(self.tabs.keys)
            let count = tabIds.count
            self.tabBarView.isHidden = false

            let barW = self.tabBarView.bounds.width
            let btnW = min(max(tabMinWidth, (barW - 80) / CGFloat(count)), tabDefaultWidth)
            var x: CGFloat = 72

            for (i, tid) in tabIds.enumerated() {
                let info = self.tabs[tid]
                let title = info?.webView.title ?? info?.title ?? "New Tab"
                let isActive = tid == self.activeTabId

                // Tab container
                let tab = NSView(frame: NSRect(x: x, y: 0, width: btnW, height: self.tabBarHeight))
                tab.wantsLayer = true
                tab.layer?.cornerRadius = 4
                self.tabBarView.effectiveAppearance.performAsCurrentDrawingAppearance {
                    tab.layer?.backgroundColor = isActive
                        ? NSColor.controlBackgroundColor.cgColor
                        : NSColor.separatorColor.withAlphaComponent(0.25).cgColor
                }
                tab.autoresizingMask = [.width, .height]
                self.tabBarView.addSubview(tab)

                // Title label
                let label = NSTextField(labelWithString: title.truncated(max: 20))
                label.frame = NSRect(x: 8, y: 4, width: btnW - 16, height: 20)
                label.autoresizingMask = [.width]
                label.font = NSFont.systemFont(ofSize: 12)
                label.textColor = isActive ? NSColor.controlTextColor : NSColor.secondaryLabelColor
                tab.addSubview(label)

                // Click area (transparent button over the whole tab)
                let clickBtn = NSButton(frame: tab.bounds)
                clickBtn.autoresizingMask = [.width, .height]
                clickBtn.bezelStyle = .shadowlessSquare
                clickBtn.isBordered = false
                clickBtn.title = ""
                clickBtn.tag = i
                clickBtn.target = self
                clickBtn.action = #selector(self.tabButtonClicked(_:))
                clickBtn.toolTip = tid
                tab.addSubview(clickBtn)

                x += btnW + 2
            }
        }
    }

    @objc func addTabClicked() {
        let tabId = createTab()
        _ = tabSwitch(tabId)
    }

    @objc func tabButtonClicked(_ sender: NSButton) {
        let tabIds = Array(tabs.keys)
        guard sender.tag >= 0, sender.tag < tabIds.count else { return }
        _ = tabSwitch(tabIds[sender.tag])
    }

    func tabSwitch(_ tabId: String) -> Bool {
        guard let tab = tabs[tabId] else { return false }
        // Hide all, show selected
        for (_, t) in tabs {
            t.webView.isHidden = t.id != tabId
        }
        tab.webView.isHidden = false
        tab.webView.window?.makeFirstResponder(tab.webView)
        activeTabId = tabId
        urlField.stringValue = displayURL(from: tab.webView.url)
        let progress = tab.webView.estimatedProgress
        progressIndicator.doubleValue = progress
        progressIndicator.isHidden = progress >= 1.0
        rebuildTabBar()
        return true
    }

    func tabList() -> [[String: String]] {
        return tabs.values.map { info in
            [
                "id": info.id,
                "title": info.webView.title ?? info.title,
                "url": info.webView.url?.absoluteString ?? "",
                "active": info.id == activeTabId ? "true" : "false"
            ]
        }
    }

    func tabClose(_ tabId: String) -> Bool {
        guard tabId != initialTabId else { return false } // can't close main tab
        guard let tab = tabs[tabId] else { return false }
        tab.webView.navigationDelegate = nil
        tab.webView.uiDelegate = nil
        tab.webView.removeObserver(self, forKeyPath: #keyPath(WKWebView.title))
        tab.webView.removeObserver(self, forKeyPath: #keyPath(WKWebView.estimatedProgress))
        tab.webView.stopLoading()
        tab.webView.removeFromSuperview()
        tabs.removeValue(forKey: tabId)

        if activeTabId == tabId {
            // Switch to main tab
            _ = tabSwitch(initialTabId)
        }
        rebuildTabBar()
        return true
    }

    func activeTabIdStr() -> String {
        return activeTabId
    }

    // MARK: - Operations (routed to active tab)

    func evaluate(script: String, timeout: Int = 10000) async throws -> String {
        try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.main.async { [weak self] in
                guard let self = self else {
                    continuation.resume(throwing: WebHostError.sessionClosed)
                    return
                }
                self.activeWebView.evaluateJavaScript(script) { result, error in
                    if let error = error {
                        continuation.resume(throwing: error)
                    } else {
                        continuation.resume(returning: result as? String ?? "")
                    }
                }
            }
        }
    }

    func evaluateSync(script: String, timeout: Int = 10000) throws -> String {
        var result: String = ""
        var evalError: Error?
        let semaphore = DispatchSemaphore(value: 0)

        DispatchQueue.main.async { [weak self] in
            guard let self = self else {
                semaphore.signal()
                return
            }
            self.activeWebView.evaluateJavaScript(script) { res, err in
                if let err = err {
                    evalError = err
                } else {
                    result = res as? String ?? ""
                }
                semaphore.signal()
            }
        }

        let timeout_ns = Int64(timeout) * Int64(NSEC_PER_MSEC)
        let didTimeout = semaphore.wait(timeout: .now() + Double(timeout_ns) / Double(NSEC_PER_SEC)) == .timedOut

        if didTimeout {
            throw WebHostError.scriptTimeout
        }
        if let err = evalError {
            throw err
        }
        return result
    }

    @MainActor func screenshot() throws -> Data {
        let wv = activeWebView
        wv.layoutSubtreeIfNeeded()
        let bounds = wv.bounds

        guard let bitmapRep = wv.bitmapImageRepForCachingDisplay(in: bounds) else {
            throw WebHostError.captureFailed
        }
        wv.cacheDisplay(in: bounds, to: bitmapRep)

        guard let pngData = bitmapRep.representation(using: .png, properties: [:]) else {
            throw WebHostError.pngConversionFailed
        }
        return pngData
    }

    func screenshotSync() throws -> Data {
        var screenshotData: Data?
        var screenshotError: Error?
        let semaphore = DispatchSemaphore(value: 0)

        DispatchQueue.main.async { [weak self] in
            guard let self = self else {
                semaphore.signal()
                return
            }
            do {
                let data = try self.screenshot()
                screenshotData = data
            } catch {
                screenshotError = error
            }
            semaphore.signal()
        }

        semaphore.wait()

        if let err = screenshotError {
            throw err
        }
        guard let data = screenshotData else {
            throw WebHostError.captureFailed
        }
        return data
    }

    func setInteractive(_ enabled: Bool) {
        interactive = enabled
        let wv = activeWebView
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            // Update border color: yellow=non-interactive, green=interactive
            if let bv = self.borderViewRef {
                bv.layer?.borderColor = enabled ? NSColor.systemGreen.cgColor : NSColor.systemYellow.cgColor
            }
            if !enabled {
                guard let parent = wv.superview ?? self.borderViewRef else { return }
                let blocker = BlockerView(frame: parent.bounds)
                blocker.autoresizingMask = [.width, .height]
                parent.addSubview(blocker)
                self.blockerView = blocker

                wv.evaluateJavaScript("""
                    window.__dolphin_block = true;
                    if (!window.__dolphin_blocker) {
                        window.__dolphin_blocker = function(e) {
                            e.preventDefault();
                            e.stopImmediatePropagation();
                        };
                        document.addEventListener('mousedown', window.__dolphin_blocker, true);
                        document.addEventListener('mouseup', window.__dolphin_blocker, true);
                        document.addEventListener('click', window.__dolphin_blocker, true);
                        document.addEventListener('keydown', window.__dolphin_blocker, true);
                        document.addEventListener('keyup', window.__dolphin_blocker, true);
                    }
                """, completionHandler: nil)
            } else {
                self.blockerView?.removeFromSuperview()
                self.blockerView = nil
                self.hostWindow.makeKeyAndOrderFront(nil)
                wv.window?.makeFirstResponder(wv)

                wv.evaluateJavaScript("""
                    window.__dolphin_block = false;
                    if (window.__dolphin_blocker) {
                        document.removeEventListener('mousedown', window.__dolphin_blocker, true);
                        document.removeEventListener('mouseup', window.__dolphin_blocker, true);
                        document.removeEventListener('click', window.__dolphin_blocker, true);
                        document.removeEventListener('keydown', window.__dolphin_blocker, true);
                        document.removeEventListener('keyup', window.__dolphin_blocker, true);
                        delete window.__dolphin_blocker;
                        delete window.__dolphin_block;
                    }
                """, completionHandler: nil)
            }
        }
    }

    func pushEvent(_ event: Event) {
        lock.lock()
        eventBuffer.append(event)
        lock.unlock()
    }

    func getEvents(since: Int64) -> [Event] {
        lock.lock()
        let events = eventBuffer.getEvents(since: since)
        lock.unlock()
        return events
    }

    func navigate(to url: URL) {
        let request = URLRequest(url: url)
        activeWebView.load(request)
    }

    func getTitle() -> String {
        return activeWebView.title ?? ""
    }

    func injectContent(css: String?, js: String?) {
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            let wv = self.activeWebView
            if let css = css, !css.isEmpty {
                let escapedCss = css.replacingOccurrences(of: "'", with: "\\'")
                let script = """
                (function() {
                    var style = document.createElement('style');
                    style.textContent = '\(escapedCss)';
                    document.head.appendChild(style);
                })();
                """
                wv.evaluateJavaScript(script, completionHandler: nil)
            }
            if let js = js, !js.isEmpty {
                wv.evaluateJavaScript(js, completionHandler: nil)
            }
        }
    }

    func waitForElement(selector: String, timeout: Int) throws -> Bool {
        let semaphore = DispatchSemaphore(value: 0)
        var found = false
        var scriptError: Error?

        let script = """
        (function() {
            var el = document.querySelector('\(selector)');
            return el ? true : false;
        })();
        """

        let start = Date()
        let timeoutSec = TimeInterval(timeout) / 1000.0

        func poll() {
            let elapsed = Date().timeIntervalSince(start)
            if elapsed >= timeoutSec {
                semaphore.signal()
                return
            }

            DispatchQueue.main.async { [weak self] in
                guard let self = self else {
                    semaphore.signal()
                    return
                }
                self.activeWebView.evaluateJavaScript(script) { res, err in
                    if let err = err {
                        scriptError = err
                        semaphore.signal()
                        return
                    }
                    if let f = res as? Bool, f {
                        found = true
                        semaphore.signal()
                        return
                    }
                    // Element not found yet, poll again after delay
                    DispatchQueue.global().asyncAfter(deadline: .now() + 0.3) {
                        poll()
                    }
                }
            }
        }

        poll()
        semaphore.wait()

        if let err = scriptError {
            throw err
        }
        return found
    }

    func resolveDialog(dialogId: String, action: String, text: String?) {
        guard let handler = dialogHandlers.removeValue(forKey: dialogId) else { return }
        let accept = action == "accept"
        handler(accept, accept ? text : nil)
    }

    private func displayURL(from url: URL?) -> String {
        guard let host = url?.host else { return "" }
        if host.hasPrefix("www.") {
            return String(host.dropFirst(4))
        }
        return host
    }

    @objc func urlFieldSubmit() {
        var text = urlField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        if !text.contains("://") { text = "https://" + text }
        if let url = URL(string: text) {
            navigate(to: url)
        }
    }

    func updateURLField() {
        urlField.stringValue = displayURL(from: activeWebView.url)
    }

    @objc func goBack() {
        activeWebView.goBack()
    }

    @objc func goForward() {
        activeWebView.goForward()
    }

    @MainActor func toggleInspector() {
        guard let inspector = activeWebView.value(forKey: "_inspector") as? NSObject else { return }
        let isVisible = (inspector.value(forKey: "isVisible") as? Bool) ?? false
        if isVisible {
            inspector.perform(Selector(("close")))
        } else {
            inspector.perform(Selector(("show")))
        }
    }

    // MARK: - NSTextFieldDelegate (URL field)

    func controlTextDidBeginEditing(_ obj: Notification) {
        guard let field = obj.object as? NSTextField, field === urlField else { return }
        urlField.stringValue = activeWebView.url?.absoluteString ?? urlField.stringValue
    }

    func controlTextDidEndEditing(_ obj: Notification) {
        guard let field = obj.object as? NSTextField, field === urlField else { return }
        urlField.stringValue = displayURL(from: activeWebView.url)
    }

    // MARK: - KVO for title changes and loading progress

    override func observeValue(forKeyPath keyPath: String?, of object: Any?, change: [NSKeyValueChangeKey: Any]?, context: UnsafeMutableRawPointer?) {
        switch keyPath {
        case #keyPath(WKWebView.title):
            DispatchQueue.main.async { [weak self] in
                guard let self = self, let wv = object as? WKWebView, let newTitle = wv.title else { return }
                for (_, tab) in self.tabs where tab.webView === wv {
                    tab.title = newTitle
                }
                self.rebuildTabBar()
            }
        case #keyPath(WKWebView.estimatedProgress):
            guard let wv = object as? WKWebView, wv === activeWebView else { return }
            let progress = wv.estimatedProgress
            progressIndicator.doubleValue = progress
            if progress >= 1.0 {
                progressIndicator.stopAnimation(nil)
                progressIndicator.isHidden = true
            } else {
                progressIndicator.isHidden = false
            }
        default:
            super.observeValue(forKeyPath: keyPath, of: object, change: change, context: context)
        }
    }
}

extension WebKitSession: NSTextFieldDelegate {}

extension WebKitSession: WKUIDelegate {
    func webView(_ webView: WKWebView,
                 runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo,
                 completionHandler: @escaping () -> Void) {
        let dialogId = UUID().uuidString
        let event = WebEvent.dialog("alert", message: message, dialogId: dialogId)
        dialogHandlers[dialogId] = { accepted, _ in
            completionHandler()
        }
        pushEvent(event)
    }

    func webView(_ webView: WKWebView,
                 runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo,
                 completionHandler: @escaping (Bool) -> Void) {
        let dialogId = UUID().uuidString
        let event = WebEvent.dialog("confirm", message: message, dialogId: dialogId)
        dialogHandlers[dialogId] = { accepted, _ in
            completionHandler(accepted)
        }
        pushEvent(event)
    }

    func webView(_ webView: WKWebView,
                 runJavaScriptTextInputPanelWithPrompt prompt: String,
                 defaultText: String?,
                 initiatedByFrame frame: WKFrameInfo,
                 completionHandler: @escaping (String?) -> Void) {
        let dialogId = UUID().uuidString
        let event = WebEvent.dialog("prompt", message: prompt, dialogId: dialogId)
        dialogHandlers[dialogId] = { _, text in
            completionHandler(text)
        }
        pushEvent(event)
    }

    func webView(_ webView: WKWebView,
                 createWebViewWith configuration: WKWebViewConfiguration,
                 for navigationAction: WKNavigationAction,
                 windowFeatures: WKWindowFeatures) -> WKWebView? {
        let url = navigationAction.request.url?.absoluteString ?? ""
        let popupId = UUID().uuidString
        let event = WebEvent.popup(url, popupId: popupId)
        pushEvent(event)

        // Create a new tab for the popup and switch to it
        let tabId = createTab(configuration: configuration)
        _ = tabSwitch(tabId)
        guard let tab = tabs[tabId] else { return nil }
        return tab.webView
    }
}

extension WebKitSession: WKNavigationDelegate {
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        let url = webView.url?.absoluteString ?? ""
        pushEvent(WebEvent.navigation(url, status: "complete"))

        // Update tab title
        for (_, tab) in tabs where tab.webView === webView {
            tab.title = webView.title ?? ""
        }

        // Update URL field if this is the active tab
        if webView === activeWebView {
            urlField.stringValue = displayURL(from: webView.url)
        }
    }

    func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) {
        let url = webView.url?.absoluteString ?? ""
        pushEvent(WebEvent.navigation(url, status: "loading"))
    }
}

enum WebHostError: Error {
    case captureFailed
    case pngConversionFailed
    case scriptTimeout
    case sessionClosed
}

class EventBuffer: Sendable {
    private var events: [Event] = []
    private let maxSize = 1000
    private let lock = NSLock()

    func append(_ event: Event) {
        lock.lock()
        if events.count >= maxSize {
            events.removeFirst()
        }
        events.append(event)
        lock.unlock()
    }

    func getEvents(since: Int64) -> [Event] {
        lock.lock()
        let result = events.filter { $0.t > since }
        lock.unlock()
        return result
    }
}

extension String {
    func truncated(max: Int) -> String {
        count <= max ? self : prefix(max) + "…"
    }
}
