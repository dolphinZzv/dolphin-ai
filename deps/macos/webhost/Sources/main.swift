import AppKit
import Foundation
import Network
import os

let server = McpServer()

let app = NSApplication.shared
app.setActivationPolicy(.regular)

func createWindow() -> NSWindow {
    let window = NSWindow(
        contentRect: NSRect(x: 0, y: 0, width: 800, height: 600),
        styleMask: [.titled, .closable, .miniaturizable, .resizable],
        backing: .buffered,
        defer: false
    )
    window.title = "WebHost - Dolphin Browser"
    window.center()

    let label = NSTextField(labelWithString: "WebHost starting...")
    label.font = NSFont.systemFont(ofSize: 16)
    label.textColor = .green
    window.contentView?.addSubview(label)
    label.translatesAutoresizingMaskIntoConstraints = false
    NSLayoutConstraint.activate([
        label.centerXAnchor.constraint(equalTo: window.contentView!.centerXAnchor),
        label.centerYAnchor.constraint(equalTo: window.contentView!.centerYAnchor)
    ])

    return window
}

var mainWindow = createWindow()

func startServer(port: UInt16 = 9223) {
    let params = NWParameters.tcp
    guard let listener = try? NWListener(using: params, on: NWEndpoint.Port(rawValue: port)!) else {
        print("Failed to create listener")
        return
    }

    listener.stateUpdateHandler = { state in
        DispatchQueue.main.async {
            switch state {
            case .ready:
                print("WebHost started on localhost:\(port)")
                mainWindow.makeKeyAndOrderFront(nil)
                if let label = mainWindow.contentView?.subviews.first as? NSTextField {
                    label.stringValue = "WebHost running on port \(port)"
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

            // Process immediately once we have a complete HTTP request,
            // without waiting for the client to close the connection.
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

        do {
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
        } catch {
            response = HttpResponse(statusCode: 500, body: "{\"error\":\"internal error\"}".data(using: .utf8)!)
        }

        sendResponse(response, conn: conn)
    }

func sendResponse(_ response: HttpResponse, conn: NWConnection) {
    conn.send(content: response.serialize(), completion: .contentProcessed { _ in
        conn.cancel()
    })
}

startServer()
app.run()