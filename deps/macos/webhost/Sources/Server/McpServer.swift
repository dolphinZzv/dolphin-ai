import Foundation
import WebKit

class McpServer {
    var sessions: [String: WebKitSession] = [:]
    let sessionManager: SessionManager = SessionManager()
    let lock = NSLock()

    func handleSync(request: JsonRpcRequest) -> JsonRpcResponse {
        switch request.method {
        case "initialize":
            return JsonRpcResponse(id: request.id, result: JsonRpcResult(
                success: true,
                extra: [
                    "protocolVersion": "2024-11-05",
                    "capabilities": ["tools": [:]] as [String: Any],
                    "serverInfo": ["name": "WebHost", "version": "0.1.0"]
                ]
            ))

        case "tools/list":
            return JsonRpcResponse(id: request.id, result: JsonRpcResult(
                success: true,
                extra: ["tools": Self.toolDefinitions]
            ))

        case "tools/call":
            guard let params = request.params,
                  let toolName = params.name else {
                return JsonRpcResponse(id: request.id, error: .invalidParams)
            }
            return handleToolCall(toolName: toolName, arguments: params.arguments, requestId: request.id)

        default:
            return JsonRpcResponse(id: request.id, error: .methodNotFound)
        }
    }

    private func handleToolCall(toolName: String, arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        switch toolName {
        case "web_session_create":
            return createSession(arguments: arguments, requestId: requestId)
        case "page_open":
            return navigate(arguments: arguments, requestId: requestId)
        case "script_run":
            return evaluate(arguments: arguments, requestId: requestId)
        case "page_screenshot":
            return screenshot(arguments: arguments, requestId: requestId)
        case "web_set_interactive":
            return setInteractive(arguments: arguments, requestId: requestId)
        case "web_capabilities":
            return getCapabilities(arguments: arguments, requestId: requestId)
        case "web_session_close":
            return closeSession(arguments: arguments, requestId: requestId)
        case "web_inject":
            return injectContent(arguments: arguments, requestId: requestId)
        case "web_wait":
            return waitForElement(arguments: arguments, requestId: requestId)
        case "web_dialog_response":
            return dialogResponse(arguments: arguments, requestId: requestId)
        case "tab_list":
            return tabList(arguments: arguments, requestId: requestId)
        case "tab_switch":
            return tabSwitch(arguments: arguments, requestId: requestId)
        case "tab_create":
            return tabCreate(arguments: arguments, requestId: requestId)
        case "tab_close":
            return tabClose(arguments: arguments, requestId: requestId)
        case "go_back":
            return goBack(arguments: arguments, requestId: requestId)
        case "go_forward":
            return goForward(arguments: arguments, requestId: requestId)
        default:
            return JsonRpcResponse(id: requestId, error: .methodNotFound)
        }
    }

    private static var toolDefinitions: [[String: Any]] {
        [
            [
                "name": "web_session_create",
                "description": "Create a new WKWebView browser session for web automation",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "viewport": [
                            "type": "object",
                            "description": "Optional viewport size for the browser window",
                            "properties": [
                                "width": ["type": "number", "description": "Viewport width in pixels"],
                                "height": ["type": "number", "description": "Viewport height in pixels"]
                            ]
                        ]
                    ]
                ] as [String: Any]
            ],
            [
                "name": "page_open",
                "description": "Navigate the browser to a specified URL",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "url": ["type": "string", "description": "URL to navigate to"],
                        "tabId": ["type": "string", "description": "Optional tab ID to operate on"]
                    ],
                    "required": ["sessionId", "url"]
                ] as [String: Any]
            ],
            [
                "name": "script_run",
                "description": "Execute JavaScript in the browser page and return results",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "script": ["type": "string", "description": "JavaScript code to execute"],
                        "timeout": ["type": "number", "description": "Timeout in milliseconds (default 10000)"],
                        "tabId": ["type": "string", "description": "Optional tab ID to operate on"]
                    ],
                    "required": ["sessionId", "script"]
                ] as [String: Any]
            ],
            [
                "name": "page_screenshot",
                "description": "Capture a screenshot of the current browser page as base64 PNG",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "tabId": ["type": "string", "description": "Optional tab ID to capture"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "web_set_interactive",
                "description": "Enable or disable interactive mode for the session",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "interactive": ["type": "boolean", "description": "Whether to enable interactive mode"],
                        "tabId": ["type": "string", "description": "Optional tab ID to toggle"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "web_capabilities",
                "description": "Get the capabilities and features supported by the browser session",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "web_session_close",
                "description": "Close a browser session and release associated resources",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "web_inject",
                "description": "Inject CSS and/or JavaScript into the current page",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "css": ["type": "string", "description": "CSS to inject into the page"],
                        "js": ["type": "string", "description": "JavaScript to inject into the page"],
                        "tabId": ["type": "string", "description": "Optional tab ID to inject into"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "web_wait",
                "description": "Wait for a DOM element matching the CSS selector to appear on the page",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "selector": ["type": "string", "description": "CSS selector to wait for"],
                        "timeout": ["type": "number", "description": "Timeout in milliseconds (default 30000)"],
                        "tabId": ["type": "string", "description": "Optional tab ID to watch"]
                    ],
                    "required": ["sessionId", "selector"]
                ] as [String: Any]
            ],
            [
                "name": "web_dialog_response",
                "description": "Respond to a JavaScript dialog (alert, confirm, or prompt)",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "dialogId": ["type": "string", "description": "Dialog ID to respond to"],
                        "action": ["type": "string", "description": "Action: accept or dismiss"],
                        "text": ["type": "string", "description": "Text to enter for prompt dialogs"],
                        "tabId": ["type": "string", "description": "Optional tab ID that has the dialog"]
                    ],
                    "required": ["sessionId", "dialogId"]
                ] as [String: Any]
            ],
            [
                "name": "tab_list",
                "description": "List all browser tabs in a session",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "tab_switch",
                "description": "Switch to a specific browser tab",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "tabId": ["type": "string", "description": "Tab ID to switch to"]
                    ],
                    "required": ["sessionId", "tabId"]
                ] as [String: Any]
            ],
            [
                "name": "tab_create",
                "description": "Create a new browser tab",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "url": ["type": "string", "description": "Optional URL to open in the new tab"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "tab_close",
                "description": "Close a browser tab",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "tabId": ["type": "string", "description": "Tab ID to close"]
                    ],
                    "required": ["sessionId", "tabId"]
                ] as [String: Any]
            ],
            [
                "name": "go_back",
                "description": "Navigate back to the previous page in the browsing history",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "tabId": ["type": "string", "description": "Optional tab ID"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ],
            [
                "name": "go_forward",
                "description": "Navigate forward to the next page in the browsing history",
                "inputSchema": [
                    "type": "object",
                    "properties": [
                        "sessionId": ["type": "string", "description": "Session ID from web_session_create"],
                        "tabId": ["type": "string", "description": "Optional tab ID"]
                    ],
                    "required": ["sessionId"]
                ] as [String: Any]
            ]
        ]
    }

    private func createSession(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        let viewport = parseViewport(arguments)
        let sessionId = UUID().uuidString

        // WKWebView and NSWindow MUST be created on the main thread.
        var session: WebKitSession?
        DispatchQueue.main.sync {
            let newSession = WebKitSession(id: sessionId, viewport: viewport)
            newSession.showWindow()
            session = newSession
        }

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .internalError)
        }

        lock.lock()
        sessions[sessionId] = session
        sessionManager.add(sessionId: sessionId)
        lock.unlock()

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, sessionId: sessionId))
    }

    private func navigate(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        guard let urlString = arguments?["url"]?.value as? String,
              let url = URL(string: urlString) else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        var title = ""
        DispatchQueue.main.sync {
            session.navigate(to: url)
            title = session.getTitle()
        }

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(
            success: true,
            url: urlString,
            title: title,
            status: "loading"
        ))
    }

    private func evaluate(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        guard let script = arguments?["script"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        let timeoutMs = (arguments?["timeout"]?.value as? Int) ?? 10000

        do {
            let result = try session.evaluateSync(script: script, timeout: timeoutMs)
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, value: result))
        } catch let err as WebHostError {
            if err == .scriptTimeout {
                return JsonRpcResponse(id: requestId, error: .scriptTimeout)
            }
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: false, value: "WebHost error: \(err)"))
        } catch {
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: false, value: "JS error: \(error.localizedDescription)"))
        }
    }

    private func screenshot(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        do {
            let data = try session.screenshotSync()
            let base64 = data.base64EncodedString()
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, data: base64))
        } catch {
            return JsonRpcResponse(id: requestId, error: .internalError)
        }
    }

    private func setInteractive(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        let interactive = arguments?["interactive"]?.value as? Bool ?? false

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        session.setInteractive(interactive)

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func getCapabilities(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        let capabilities: [String: AnyCodable] = [
            "dialog": AnyCodable(true),
            "popup": AnyCodable(true),
            "screenshot": AnyCodable(true),
            "fullPage": AnyCodable(true),
            "console": AnyCodable(true),
            "navigation": AnyCodable(true),
            "upload": AnyCodable(true),
            "download": AnyCodable(false)
        ]

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, capabilities: capabilities))
    }

    private func closeSession(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            // Idempotent close: session already gone, return success.
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
        }

        // Cleanup and release on main thread. cleanup() must be called while
        // the session is still alive (before removal from dictionary) to
        // prevent WKWebView delegate callbacks from firing on a
        // partially-deallocated object.
        let work = {
            session.cleanup()
            self.lock.lock()
            self.sessions.removeValue(forKey: sessionId)
            self.lock.unlock()
        }
        if Thread.isMainThread {
            work()
        } else {
            DispatchQueue.main.sync { work() }
        }

        sessionManager.remove(sessionId: sessionId)

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func injectContent(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        let css = arguments?["css"]?.value as? String
        let js = arguments?["js"]?.value as? String

        session.injectContent(css: css, js: js)

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func waitForElement(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        guard let selector = arguments?["selector"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        let timeout = (arguments?["timeout"]?.value as? Int) ?? 30000

        do {
            let found = try session.waitForElement(selector: selector, timeout: timeout)
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, found: found))
        } catch {
            return JsonRpcResponse(id: requestId, error: .navigationTimeout)
        }
    }

    private func dialogResponse(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        let dialogId = arguments?["dialogId"]?.value as? String
        let action = arguments?["action"]?.value as? String
        let text = arguments?["text"]?.value as? String

        session.resolveDialog(dialogId: dialogId ?? "", action: action ?? "dismiss", text: text)

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func tabList(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        var tabs: [[String: String]] = []
        DispatchQueue.main.sync {
            tabs = session.tabList()
        }

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, extra: ["tabs": tabs]))
    }

    private func tabSwitch(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String,
              let tabId = arguments?["tabId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        var success = false
        DispatchQueue.main.sync {
            success = session.tabSwitch(tabId)
        }

        if !success {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func tabCreate(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        let url = arguments?["url"]?.value as? String
        var tabId = ""
        DispatchQueue.main.sync {
            tabId = session.createTab()
            if let urlStr = url, let url = URL(string: urlStr) {
                session.navigate(to: url)
            }
        }

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, extra: ["tabId": tabId]))
    }

    private func tabClose(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String,
              let tabId = arguments?["tabId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        var success = false
        DispatchQueue.main.sync {
            success = session.tabClose(tabId)
        }

        if !success {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func goBack(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        DispatchQueue.main.sync {
            session.goBack()
        }

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func goForward(arguments: [String: AnyCodable]?, requestId: Any?) -> JsonRpcResponse {
        guard let sessionId = arguments?["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        if !switchToTabIfNeeded(arguments: arguments, session: session) {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        DispatchQueue.main.sync {
            session.goForward()
        }

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func parseViewport(_ arguments: [String: AnyCodable]?) -> Viewport {
        guard let args = arguments,
              let vp = args["viewport"]?.value as? [String: Any],
              let width = vp["width"] as? Int,
              let height = vp["height"] as? Int else {
            return Viewport()
        }
        return Viewport(width: width, height: height)
    }

    /// Switch to a specific tab if tabId is provided. Returns false if the tab doesn't exist.
    private func switchToTabIfNeeded(arguments: [String: AnyCodable]?, session: WebKitSession) -> Bool {
        guard let tabId = arguments?["tabId"]?.value as? String else { return true }
        var ok = false
        DispatchQueue.main.sync {
            ok = session.tabSwitch(tabId)
        }
        return ok
    }

    func toggleInspectorAll() {
        lock.lock()
        let activeSessions = Array(sessions.values)
        lock.unlock()
        for session in activeSessions {
            DispatchQueue.main.async {
                session.toggleInspector()
            }
        }
    }
}

class SessionManager: Sendable {
    private var activeSessions: Set<String> = []
    private let lock = NSLock()
    let maxCount = 10

    func add(sessionId: String) {
        lock.lock()
        activeSessions.insert(sessionId)
        lock.unlock()
    }

    func remove(sessionId: String) {
        lock.lock()
        activeSessions.remove(sessionId)
        lock.unlock()
    }

    func count() -> Int {
        lock.lock()
        let c = activeSessions.count
        lock.unlock()
        return c
    }
}