import AppKit
import Foundation
import Network
import os
import SnapKit

let server = McpServer()

// MARK: - App Delegate

class AppDelegate: NSObject, NSApplicationDelegate {
    var statusItem: NSStatusItem!
    var mainWindow: NSWindow!

    func applicationDidFinishLaunching(_ notification: Notification) {
        mainWindow = createWindow()
        setupStatusBar()

        // Keyboard shortcuts: Cmd+T new tab, Cmd+W close tab
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self = self else { return event }
            guard event.modifierFlags.contains(.command) else { return event }

            switch event.charactersIgnoringModifiers {
            case "t":
                self.createNewTab()
                return nil
            case "w":
                self.closeActiveTab()
                return nil
            default:
                return event
            }
        }
    }

    func setupStatusBar() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)

        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "globe", accessibilityDescription: "WebHost")
        }

        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "WebHost Running", action: nil, keyEquivalent: ""))
        menu.addItem(NSMenuItem.separator())

        let showItem = NSMenuItem(title: "Show Window", action: #selector(toggleWindow), keyEquivalent: "w")
        showItem.target = self
        menu.addItem(showItem)

        let openItem = NSMenuItem(title: "Open in Browser", action: #selector(openInBrowser), keyEquivalent: "o")
        openItem.target = self
        menu.addItem(openItem)

        let inspectorItem = NSMenuItem(title: "Toggle Web Inspector", action: #selector(toggleWebInspector), keyEquivalent: "i")
        inspectorItem.target = self
        menu.addItem(inspectorItem)

        menu.addItem(NSMenuItem.separator())
        let quitItem = NSMenuItem(title: "Quit WebHost", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        menu.addItem(quitItem)

        statusItem.menu = menu
    }

    @objc func toggleWindow() {
        guard let window = mainWindow else { return }
        if window.isVisible {
            window.orderOut(nil)
        } else {
            window.makeKeyAndOrderFront(nil)
            NSApplication.shared.activate(ignoringOtherApps: true)
        }
    }

    @objc func openInBrowser() {
        if let url = URL(string: "http://localhost:9223/health") {
            NSWorkspace.shared.open(url)
        }
    }

    @objc func toggleWebInspector() {
        server.toggleInspectorAll()
    }

    @objc func createNewTab() {
        guard let session = firstActiveSession() else { return }
        DispatchQueue.main.async {
            let tabId = session.createTab()
            _ = session.tabSwitch(tabId)
        }
    }

    @objc func closeActiveTab() {
        guard let session = firstActiveSession() else { return }
        let tabId = session.activeTabIdStr()
        guard tabId != "main" else { return }
        DispatchQueue.main.async {
            _ = session.tabClose(tabId)
        }
    }

    private func firstActiveSession() -> WebKitSession? {
        server.lock.lock()
        defer { server.lock.unlock() }
        return server.sessions.first?.value
    }
}

let delegate = AppDelegate()
NSApplication.shared.delegate = delegate
NSApplication.shared.setActivationPolicy(.accessory)

func createWindow() -> NSWindow {
    let window = NSWindow(
        contentRect: NSRect(x: 0, y: 0, width: 800, height: 600),
        styleMask: [.titled, .closable, .miniaturizable, .resizable],
        backing: .buffered,
        defer: false
    )
    window.title = "WebHost - Dolphin Browser"
    window.center()

    guard let contentView = window.contentView else { return window }
    let stack = NSStackView(frame: contentView.bounds)
    stack.orientation = .vertical
    stack.spacing = 8
    stack.alignment = .centerX
    stack.distribution = .fill

    let imgView: NSImageView
    if let image = NSImage(systemSymbolName: "globe", accessibilityDescription: nil) {
        imgView = NSImageView(image: image)
    } else {
        imgView = NSImageView()
    }
    imgView.setFrameSize(NSSize(width: 48, height: 48))
    imgView.contentTintColor = .controlAccentColor
    let iconView: NSView = imgView

    let statusLabel = NSTextField(labelWithString: "WebHost starting...")
    statusLabel.font = NSFont.systemFont(ofSize: 14)
    statusLabel.textColor = .secondaryLabelColor
    statusLabel.tag = 42

    let portLabel = NSTextField(labelWithString: "Port: 9223")
    portLabel.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
    portLabel.textColor = .tertiaryLabelColor

    stack.addArrangedSubview(NSView(frame: .zero)) // spacer
    stack.addArrangedSubview(iconView)
    stack.addArrangedSubview(statusLabel)
    stack.addArrangedSubview(portLabel)

    contentView.addSubview(stack)
    stack.snp.makeConstraints { make in
        make.centerX.centerY.equalTo(contentView)
        make.leading.greaterThanOrEqualTo(contentView).offset(40)
        make.trailing.lessThanOrEqualToSuperview().offset(-40)
    }

    return window
}

func startServer(port: UInt16 = 9223) {
    let params = NWParameters.tcp
    guard let port = NWEndpoint.Port(rawValue: port), port.rawValue > 0 else {
        print("Invalid port: \(port)")
        return
    }
    guard let listener = try? NWListener(using: params, on: port) else {
        print("Failed to create listener")
        return
    }

    listener.stateUpdateHandler = { state in
        DispatchQueue.main.async {
            switch state {
            case .ready:
                print("WebHost started on localhost:\(port)")
                if let label = delegate.mainWindow?.contentView?.viewWithTag(42) as? NSTextField {
                    label.stringValue = "WebHost running on port \(port)"
                    label.textColor = .green
                }
            case .failed(let err):
                print("Listener failed: \(err.localizedDescription)")
            default:
                break
            }
        }
    }

    listener.newConnectionHandler = { conn in
        conn.stateUpdateHandler = { _ in }
        conn.start(queue: DispatchQueue.global(qos: .userInitiated))
        handleConnection(conn)
    }

    listener.start(queue: DispatchQueue.global(qos: .userInitiated))
}

func handleConnection(_ conn: NWConnection) {
    var receivedData = Data()

    func receiveNext() {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 32768) { data, _, isComplete, error in
            if let data = data { receivedData.append(data) }

            if let request = parseRequest(receivedData) {
                handleRequest(request, conn: conn)
                return
            }

            if error != nil || isComplete {
                conn.cancel()
            } else {
                receiveNext()
            }
        }
    }
    receiveNext()
}

func parseRequest(_ data: Data) -> HttpRequest? {
    guard let headerEnd = data.range(of: Data("\r\n\r\n".utf8)) else { return nil }
    guard let headerStr = String(data: data[..<headerEnd.lowerBound], encoding: .utf8) else { return nil }

    let lines = headerStr.components(separatedBy: "\r\n")
    guard let requestLine = lines.first else { return nil }
    let parts = requestLine.split(separator: " ", maxSplits: 2)
    guard parts.count >= 2 else { return nil }

    var headers: [String: String] = [:]
    for line in lines.dropFirst() {
        if let colon = line.range(of: ": ") {
            headers[String(line[..<colon.lowerBound]).lowercased()] = String(line[colon.upperBound...])
        }
    }

    let contentLength = Int(headers["content-length"] ?? "0") ?? 0
    let bodyStart = headerEnd.upperBound
    let body: Data? = contentLength > 0 && data.count >= bodyStart + contentLength
        ? data[bodyStart..<(bodyStart + contentLength)]
        : (data.count > bodyStart ? data[bodyStart...] : nil)

    return HttpRequest(method: String(parts[0]), uri: String(parts[1]), headers: headers, body: body)
}

struct HttpRequest {
    let method: String
    let uri: String
    let headers: [String: String]
    let body: Data?
}

struct HttpResponse {
    let statusCode: Int
    let body: Data

    var statusMessage: String {
        switch statusCode {
        case 200: return "OK"
        case 400: return "Bad Request"
        case 404: return "Not Found"
        case 500: return "Internal Server Error"
        default: return "Unknown"
        }
    }

    func serialize() -> Data {
        let statusLine = "HTTP/1.1 \(statusCode) \(statusMessage)\r\n"
        let headers = "Content-Type: application/json\r\nContent-Length: \(body.count)\r\nConnection: close\r\n\r\n"
        var result = Data()
        result.append((statusLine + headers).data(using: .utf8)!)
        result.append(body)
        return result
    }
}

func handleRequest(_ request: HttpRequest, conn: NWConnection) {
        let response: HttpResponse

        if request.uri == "/health" && request.method == "GET" {
                response = HttpResponse(statusCode: 200, body: "{\"status\":\"ok\"}".data(using: .utf8)!)
            }
            else if request.uri == "/mcp/call" && request.method == "POST" {
                guard let bodyData = request.body,
                      let json = try? JSONSerialization.jsonObject(with: bodyData) as? [String: Any],
                      json["jsonrpc"] as? String == "2.0",
                      let id = json["id"],
                      let method = json["method"] as? String else {
                    response = HttpResponse(statusCode: 400, body: "{\"error\":\"invalid request\"}".data(using: .utf8)!)
                    sendResponse(response, conn: conn)
                    return
                }

                let params = json["params"] as? [String: Any]
                let rpcRequest = JsonRpcRequest(id: id, method: method, params: params)
                let rpcResponse = server.handleSync(request: rpcRequest)
                response = HttpResponse(statusCode: 200, body: rpcResponse.toJson().data(using: .utf8)!)
            }
            else if request.uri == "/mcp/sessions" && request.method == "GET" {
                var sessionList: [[String: Any]] = []
                server.lock.lock()
                for (id, _) in server.sessions {
                    sessionList.append(["sessionId": id, "active": true])
                }
                server.lock.unlock()

                if let jsonData = try? JSONSerialization.data(withJSONObject: sessionList),
                   let jsonStr = String(data: jsonData, encoding: .utf8) {
                    response = HttpResponse(statusCode: 200, body: jsonStr.data(using: .utf8)!)
                } else {
                    response = HttpResponse(statusCode: 500, body: "{\"error\":\"internal error\"}".data(using: .utf8)!)
                }
            }
            else if request.uri.hasPrefix("/mcp/sessions/") && request.method == "DELETE" {
                let sessionId = String(request.uri.dropFirst("/mcp/sessions/".count))
                server.lock.lock()
                let existed = server.sessions[sessionId] != nil
                server.sessions.removeValue(forKey: sessionId)
                server.lock.unlock()

                if existed {
                    server.sessionManager.remove(sessionId: sessionId)
                    response = HttpResponse(statusCode: 200, body: "{\"success\":true}".data(using: .utf8)!)
                } else {
                    response = HttpResponse(statusCode: 404, body: "{\"error\":\"session not found\"}".data(using: .utf8)!)
                }
            }
            else if request.uri.hasPrefix("/mcp/stream") && request.method == "GET" {
                guard let query = request.uri.split(separator: "?").dropFirst().first.flatMap(String.init) else {
                    response = HttpResponse(statusCode: 400, body: "{\"error\":\"missing sessionId\"}".data(using: .utf8)!)
                    sendResponse(response, conn: conn)
                    return
                }

                var sessionId: String?
                for param in query.split(separator: "&") {
                    let kv = param.split(separator: "=", maxSplits: 1)
                    if kv.count == 2 && kv[0] == "sessionId" { sessionId = String(kv[1]) }
                }

                guard let sid = sessionId else {
                    response = HttpResponse(statusCode: 400, body: "{\"error\":\"missing sessionId\"}".data(using: .utf8)!)
                    sendResponse(response, conn: conn)
                    return
                }

                server.lock.lock()
                guard let session = server.sessions[sid] else {
                    server.lock.unlock()
                    response = HttpResponse(statusCode: 404, body: "{\"error\":\"session not found\"}".data(using: .utf8)!)
                    sendResponse(response, conn: conn)
                    return
                }
                let events = session.getEvents(since: 0)
                server.lock.unlock()

                var body = ""
                for e in events { body += e.toJson() }
                response = HttpResponse(statusCode: 200, body: body.data(using: .utf8)!)
            }
            else {
                response = HttpResponse(statusCode: 404, body: "{\"error\":\"not found\"}".data(using: .utf8)!)
            }
        sendResponse(response, conn: conn)
    }

func sendResponse(_ response: HttpResponse, conn: NWConnection) {
    conn.send(content: response.serialize(), completion: .contentProcessed { _ in
        conn.cancel()
    })
}

// MARK: - App Lifecycle

let port: UInt16
if let portArg = CommandLine.arguments.last,
   let p = UInt16(portArg),
   p > 0 {
    port = p
} else if let envPort = ProcessInfo.processInfo.environment["WEBHOST_PORT"],
          let p = UInt16(envPort),
          p > 0 {
    port = p
} else {
    port = 9223
}
startServer(port: port)
NSApplication.shared.run()
