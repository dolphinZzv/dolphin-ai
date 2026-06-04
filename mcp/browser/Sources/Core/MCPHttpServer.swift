import Foundation
import Network

// MARK: - Minimal HTTP request/response

private struct HTTPRequest {
    let method: String
    let path: String
    let headers: [String: String]
    let body: Data
}

private struct HTTPResponse {
    let status: Int
    let headers: [(String, String)]
    let body: Data
}

// MARK: - JSON-RPC types

struct JSONRPCRequest: Codable {
    let jsonrpc: String
    let id: Int
    let method: String
    let params: Params?

    struct Params: Codable {
        let name: String?
        let arguments: [String: JSONValue]?
    }
}

enum JSONValue: Codable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case null

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let str = try? container.decode(String.self) {
            self = .string(str)
        } else if let num = try? container.decode(Double.self) {
            self = .number(num)
        } else if let bool = try? container.decode(Bool.self) {
            self = .bool(bool)
        } else if container.decodeNil() {
            self = .null
        } else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "unexpected JSON value")
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .string(let s): try container.encode(s)
        case .number(let n): try container.encode(n)
        case .bool(let b): try container.encode(b)
        case .null: try container.encodeNil()
        }
    }

    var stringValue: String? {
        if case .string(let s) = self { return s }
        if case .number(let n) = self { return String(format: "%g", n) }
        return nil
    }

    var numberValue: Double? {
        if case .number(let n) = self { return n }
        return nil
    }
}

// MARK: - Tool definitions

struct ToolDefinition: Encodable {
    let name: String
    let description: String
    let inputSchema: InputSchema
}

struct InputSchema: Encodable {
    let type = "object"
    let properties: [String: PropertySchema]
    let required: [String]?
}

struct PropertySchema: Encodable {
    let type: String
    let description: String
    let `default`: String?
}

// MARK: - Tool registry

typealias ToolArgs = [String: JSONValue]

/// Encode a JSON object as a string for tool results.
private func toolResult(_ dict: [String: Any]) -> String {
    let data = try! JSONSerialization.data(withJSONObject: dict)
    return String(data: data, encoding: .utf8)!
}

struct Tool: @unchecked Sendable {
    let name: String
    let description: String
    let properties: [String: PropertySchema]
    let required: [String]?
    let validate: (ToolArgs) -> String? // return error message or nil
    let run: (ToolArgs, WebViewModel, TaskManager) async throws -> String
}

private func makeTools(viewModel: WebViewModel, taskManager: TaskManager) -> [Tool] {
    [
        Tool(
            name: "browser_navigate",
            description: "Navigate to a URL in the specified tab (or active tab if not specified)",
            properties: [
                "url": PropertySchema(type: "string", description: "Target URL", default: nil),
                "tab_id": PropertySchema(type: "string", description: "Tab ID (optional, defaults to active tab)", default: nil),
            ],
            required: ["url"],
            validate: { args in
                if args["url"]?.stringValue?.isEmpty ?? true { return "url is required" }
                return nil
            },
            run: { args, vm, _ in
                let url = args["url"]!.stringValue!
                let tabId = args["tab_id"]?.stringValue
                try await vm.navigate(to: url, in: tabId)
                return toolResult(["status": "ok", "url": url])
            }
        ),
        Tool(
            name: "browser_evaluate",
            description: "Execute JavaScript on the specified page (or active tab) and return the result",
            properties: [
                "expression": PropertySchema(type: "string", description: "JavaScript expression to evaluate", default: nil),
                "tab_id": PropertySchema(type: "string", description: "Tab ID (optional, defaults to active tab)", default: nil),
            ],
            required: ["expression"],
            validate: { args in
                if args["expression"]?.stringValue?.isEmpty ?? true { return "expression is required" }
                return nil
            },
            run: { args, vm, _ in
                let expr = args["expression"]!.stringValue!
                let tabId = args["tab_id"]?.stringValue
                let result = try await vm.evaluate(script: expr, in: tabId)
                return toolResult(["result": result])
            }
        ),
        Tool(
            name: "browser_screenshot",
            description: "Take a screenshot and save to a file, return the file path",
            properties: [
                "url": PropertySchema(type: "string", description: "Optional URL to navigate to first", default: nil),
                "output_dir": PropertySchema(type: "string", description: "Screenshot output directory, default: screenshots", default: nil),
                "tab_id": PropertySchema(type: "string", description: "Tab ID (optional, defaults to active tab)", default: nil),
            ],
            required: nil,
            validate: { _ in nil },
            run: { args, vm, _ in
                let url = args["url"]?.stringValue
                let tabId = args["tab_id"]?.stringValue
                let outputDir = args["output_dir"]?.stringValue ?? "screenshots"
                let path = try await vm.screenshot(url: url, outputDir: outputDir, in: tabId)
                return toolResult(["path": path])
            }
        ),
        Tool(
            name: "browser_set_window_size",
            description: "Resize the browser window to the given width and height in pixels",
            properties: [
                "width": PropertySchema(type: "number", description: "Window width in pixels, min 400", default: nil),
                "height": PropertySchema(type: "number", description: "Window height in pixels, min 300", default: nil),
            ],
            required: ["width", "height"],
            validate: { args in
                guard let w = args["width"]?.numberValue, let h = args["height"]?.numberValue else {
                    return "width and height are required"
                }
                if w < 400 || h < 300 { return "width must be >= 400, height must be >= 300" }
                return nil
            },
            run: { args, vm, _ in
                let w = Int(args["width"]!.numberValue!)
                let h = Int(args["height"]!.numberValue!)
                await MainActor.run {
                    vm.windowWidth = CGFloat(w)
                    vm.windowHeight = CGFloat(h)
                    vm.applyWindowSize()
                }
                return toolResult(["status": "ok", "width": w, "height": h])
            }
        ),
        Tool(
            name: "browser_create_tab",
            description: "Create a new browser tab and return its tab_id",
            properties: [:],
            required: nil,
            validate: { _ in nil },
            run: { args, vm, _ in
                let tabId = await vm.createTab()
                return toolResult(["status": "ok", "tab_id": tabId])
            }
        ),
        Tool(
            name: "browser_close_tab",
            description: "Close a browser tab by tab_id. At least one tab must remain open.",
            properties: ["tab_id": PropertySchema(type: "string", description: "Tab ID to close", default: nil)],
            required: ["tab_id"],
            validate: { args in
                if args["tab_id"]?.stringValue?.isEmpty ?? true { return "tab_id is required" }
                return nil
            },
            run: { args, vm, _ in
                let tabId = args["tab_id"]!.stringValue!
                await vm.closeTab(id: tabId)
                return toolResult(["status": "ok", "tab_id": tabId])
            }
        ),
        Tool(
            name: "browser_list_tabs",
            description: "List all open browser tabs with their IDs, URLs, and titles",
            properties: [:],
            required: nil,
            validate: { _ in nil },
            run: { args, vm, _ in
                let rows: [(id: String, url: String, title: String, isActive: Bool)] = await MainActor.run {
                    vm.tabOrder.compactMap { id in
                        guard let tab = vm.tabs[id] else { return nil }
                        return (id, tab.url, tab.title, id == vm.activeTabId)
                    }
                }
                let tabList: [[String: Any]] = rows.map { r in
                    ["tab_id": r.id, "url": r.url, "title": r.title, "is_active": r.isActive]
                }
                return toolResult(["tabs": tabList])
            }
        ),
        Tool(
            name: "browser_activate_tab",
            description: "Switch the active tab by tab_id. This brings the tab into view.",
            properties: ["tab_id": PropertySchema(type: "string", description: "Tab ID to activate", default: nil)],
            required: ["tab_id"],
            validate: { args in
                if args["tab_id"]?.stringValue?.isEmpty ?? true { return "tab_id is required" }
                return nil
            },
            run: { args, vm, _ in
                let tabId = args["tab_id"]!.stringValue!
                await vm.activateTab(id: tabId)
                return toolResult(["status": "ok", "tab_id": tabId])
            }
        ),
        Tool(
            name: "browser_get_task_result",
            description: "Poll a task by taskId and return the result. Use this after calling a browser tool with async=1.",
            properties: ["taskId": PropertySchema(type: "string", description: "Task ID returned from an async browser tool call", default: nil)],
            required: ["taskId"],
            validate: { args in
                if args["taskId"]?.stringValue?.isEmpty ?? true { return "taskId is required" }
                return nil
            },
            run: { args, vm, tm in
                let taskId = args["taskId"]!.stringValue!
                guard let task = tm.get(id: taskId) else {
                    return toolResult(["error": "task not found", "taskId": taskId])
                }
                switch task.state {
                case .pending, .running:
                    return toolResult(["status": "processing", "taskId": taskId])
                case .completed(let jsonStr):
                    return jsonStr
                case .failed(let code, let msg):
                    return toolResult(["error": ["code": code, "message": msg], "taskId": taskId])
                }
            }
        ),
    ]
}

// MARK: - MCPHttpServer

public final class MCPHttpServer: @unchecked Sendable {
    private let viewModel: WebViewModel
    private let port: UInt16
    private var listener: NWListener?
    private let queue = DispatchQueue(label: "mcp-http")
    private let sse = SSEManager()

    private let taskManager = TaskManager()
    private let tools: [Tool]

    public init(viewModel: WebViewModel, port: UInt16 = 9876) {
        self.viewModel = viewModel
        self.port = port
        self.tools = makeTools(viewModel: viewModel, taskManager: taskManager)
    }

    deinit {
        stop()
    }

    public func start() throws {
        let params = NWParameters.tcp
        params.allowLocalEndpointReuse = true

        let listener = try NWListener(using: params, on: NWEndpoint.Port(rawValue: port)!)
        self.listener = listener

        listener.stateUpdateHandler = { [weak self] state in
            if case .failed(let error) = state {
                print("[MCP HTTP] listener error: \(error)")
                self?.restart()
            }
        }

        listener.newConnectionHandler = { [weak self] connection in
            self?.handle(connection)
        }

        listener.start(queue: queue)
        print("[MCP HTTP] listening on port \(port)")
    }

    public func stop() {
        listener?.cancel()
        listener = nil
        for (_, conn) in sse.all {
            conn.cancel()
        }
        sse.removeAll()
    }

    private func restart() {
        stop()
        try? start()
    }

    // MARK: - Connection handling

    private func handle(_ connection: NWConnection) {
        connection.start(queue: queue)
        readRequest(connection)
    }

    private func readRequest(_ connection: NWConnection) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 2_000_000) { [weak self] data, _, isComplete, error in
            guard let self, let data = data, !data.isEmpty else {
                if isComplete || error != nil {
                    connection.cancel()
                }
                return
            }

            // Parse HTTP request
            guard let request = self.parseHTTPRequest(data) else {
                self.sendResponse(connection, HTTPResponse(status: 400, headers: [("Content-Type", "text/plain")], body: Data("Bad Request".utf8)))
                return
            }

            self.route(connection, request)
        }
    }

    private func route(_ connection: NWConnection, _ request: HTTPRequest) {
        switch (request.method, request.path) {
        case ("GET", "/sse"):
            handleSSE(connection, request)
        case ("POST", "/message"), ("POST", "/sse"):
            handleMessage(connection, request)
        case ("GET", let path) where path.hasPrefix("/task/"):
            handleTaskStatus(connection, path: path)
        default:
            sendResponse(connection, HTTPResponse(status: 404, headers: [("Content-Type", "text/plain")], body: Data("Not Found".utf8)))
        }
    }

    // MARK: - SSE

    private func handleSSE(_ connection: NWConnection, _ request: HTTPRequest) {
        let id = UUID()
        sse[id] = connection

        // SSE headers
        var resp = "HTTP/1.1 200 OK\r\n"
        resp += "Content-Type: text/event-stream\r\n"
        resp += "Cache-Control: no-cache\r\n"
        resp += "Connection: keep-alive\r\n"
        resp += "Access-Control-Allow-Origin: *\r\n"
        resp += "\r\n"

        connection.send(content: resp.data(using: .utf8), completion: .idempotent)

        // Send initial endpoint event
        let endpointEvent = "event: endpoint\ndata: /message?sessionId=\(id.uuidString)\n\n"
        connection.send(content: endpointEvent.data(using: .utf8), completion: .idempotent)

        // Keep connection alive and read for close detection
        keepSSEAlive(connection, id: id)
    }

    private func keepSSEAlive(_ connection: NWConnection, id: UUID) {
        // Continuously read to detect connection close
        connection.receive(minimumIncompleteLength: 1, maximumLength: 1) { [weak self] _, _, isComplete, error in
            if isComplete || error != nil {
                self?.sse.removeValue(forKey: id)
                connection.cancel()
                return
            }
            // read again
            self?.keepSSEAlive(connection, id: id)
        }
    }

    // MARK: - POST /message

    private func handleMessage(_ connection: NWConnection, _ request: HTTPRequest) {
        guard let rpcRequest = try? JSONDecoder().decode(JSONRPCRequest.self, from: request.body) else {
            sendJSONRPCError(connection, id: nil, code: -32700, message: "Parse error")
            return
        }
        let isAsync = request.path.contains("async=1")

        switch rpcRequest.method {
        case "tools/list":
            let toolsJSON = handleList()
            sendJSONRPCListResult(connection, id: rpcRequest.id, toolsJSON: toolsJSON)

        case "tools/call":
            handleCallAsync(connection, rpcRequest, isAsync: isAsync)

        default:
            sendJSONRPCError(connection, id: rpcRequest.id, code: -32601, message: "method not found")
        }
    }

    // MARK: - tools/list

    private func handleList() -> String {
        let defs: [ToolDefinition] = tools.map { t in
            ToolDefinition(
                name: t.name,
                description: t.description,
                inputSchema: InputSchema(
                    properties: t.properties,
                    required: t.required
                )
            )
        }
        let data = try! JSONEncoder().encode(["tools": defs])
        return String(data: data, encoding: .utf8)!
    }

    // MARK: - tools/call

    private func handleCallAsync(_ connection: NWConnection, _ request: JSONRPCRequest, isAsync: Bool) {
        guard let params = request.params, let name = params.name else {
            sendJSONRPCError(connection, id: request.id, code: -32602, message: "invalid params")
            return
        }
        guard let tool = tools.first(where: { $0.name == name }) else {
            sendJSONRPCError(connection, id: request.id, code: -32601, message: "tool not found")
            return
        }

        let args = params.arguments ?? [:]
        if let err = tool.validate(args) {
            sendJSONRPCError(connection, id: request.id, code: -32602, message: err)
            return
        }

        // browser_set_window_size is synchronous
        if name == "browser_set_window_size" {
            let w = Int(args["width"]!.numberValue!)
            let h = Int(args["height"]!.numberValue!)
            Task { @MainActor in
                viewModel.windowWidth = CGFloat(w)
                viewModel.windowHeight = CGFloat(h)
                viewModel.applyWindowSize()
            }
            sendJSONRPCResult(connection, id: request.id, result: toolResult(["status": "ok", "width": w, "height": h]))
            return
        }

        if isAsync {
            let taskId = UUID().uuidString
            taskManager.add(id: taskId, name: name, args: args, rpcId: request.id)
            sendJSONRPCResult(connection, id: request.id, result: toolResult(["taskId": taskId]))
            processNextTask()
        } else {
            beginSSE(connection)
            Task { [weak self] in
                guard let self else { return }
                do {
                    let result = try await tool.run(args, viewModel, taskManager)
                    self.sendSSEResult(connection, id: request.id, result: result)
                } catch {
                    self.sendSSEError(connection, id: request.id, code: -1, message: error.localizedDescription)
                }
            }
        }
    }


    // MARK: - Task status polling

    private func handleTaskStatus(_ connection: NWConnection, path: String) {
        let taskId = String(path.dropFirst("/task/".count))
        guard !taskId.isEmpty else {
            sendJSON(connection, body: makeErrorBody(id: nil, code: -32602, message: "taskId is required"))
            return
        }
        guard let task = taskManager.get(id: taskId) else {
            sendJSON(connection, body: makeErrorBody(id: nil, code: -1, message: "task not found"))
            return
        }
        switch task.state {
        case .pending, .running:
            let text = toolResult(["status": "processing", "taskId": taskId])
            let contentItem: [String: String] = ["type": "text", "text": text]
            let body: [String: Any] = [
                "jsonrpc": "2.0",
                "id": task.rpcId,
                "result": ["content": [contentItem], "status": "processing"],
            ]
            sendJSON(connection, body: body)
        case .completed(let jsonStr):
            if let data = jsonStr.data(using: .utf8),
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                sendJSON(connection, body: obj)
            } else {
                sendJSON(connection, body: makeErrorBody(id: task.rpcId, code: -1, message: "internal error"))
            }
        case .failed(let code, let msg):
            sendJSON(connection, body: makeErrorBody(id: task.rpcId, code: code, message: msg))
        }
    }

    // MARK: - Background task execution

    private func processNextTask() {
        Task { [weak self] in
            guard let self else { return }
            while let taskId = taskManager.nextPending() {
                taskManager.update(id: taskId, state: .running)
                guard let task = taskManager.get(id: taskId) else { continue }
                await executeTask(task, taskId: taskId)
            }
        }
    }

    private func executeTask(_ task: ManagedTask, taskId: String) async {
        guard let tool = tools.first(where: { $0.name == task.name }) else {
            taskManager.update(id: taskId, state: .failed(-32601, "tool not found"))
            return
        }
        do {
            let result = try await tool.run(task.arguments, viewModel, taskManager)
            storeResult(taskId: taskId, rpcId: task.rpcId, contentText: result)
        } catch {
            taskManager.update(id: taskId, state: .failed(-1, error.localizedDescription))
        }
    }

    private func storeResult(taskId: String, rpcId: Int, contentText: String) {
        let contentItem: [String: String] = ["type": "text", "text": contentText]
        let body: [String: Any] = [
            "jsonrpc": "2.0",
            "id": rpcId,
            "result": ["content": [contentItem]],
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: body),
              let jsonStr = String(data: data, encoding: .utf8) else { return }
        taskManager.update(id: taskId, state: .completed(jsonStr))
    }

    private func makeErrorBody(id: Int?, code: Int, message: String) -> [String: Any] {
        var body: [String: Any] = [
            "jsonrpc": "2.0",
            "error": ["code": code, "message": message],
        ]
        if let id = id {
            body["id"] = id
        } else {
            body["id"] = NSNull()
        }
        return body
    }

    // MARK: - SSE streaming for tools/call

    private func beginSSE(_ connection: NWConnection) {
        let header = "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\nConnection: keep-alive\r\n\r\n"
        connection.send(content: header.data(using: .utf8), completion: .idempotent)
    }

    private func sendSSEResult(_ connection: NWConnection, id: Int, result: String) {
        let body: [String: Any] = [
            "jsonrpc": "2.0",
            "id": id,
            "result": ["content": [["type": "text", "text": result]]],
        ]
        guard let jsonData = try? JSONSerialization.data(withJSONObject: body),
              let jsonStr = String(data: jsonData, encoding: .utf8) else { return }
        let event = "event: result\ndata: \(jsonStr)\n\n"
        connection.send(content: event.data(using: .utf8), completion: .contentProcessed { _ in
            connection.cancel()
        })
    }

    private func sendSSEError(_ connection: NWConnection, id: Int, code: Int, message: String) {
        let body: [String: Any] = [
            "jsonrpc": "2.0",
            "id": id,
            "error": ["code": code, "message": message],
        ]
        guard let jsonData = try? JSONSerialization.data(withJSONObject: body),
              let jsonStr = String(data: jsonData, encoding: .utf8) else { return }
        let event = "event: error\ndata: \(jsonStr)\n\n"
        connection.send(content: event.data(using: .utf8), completion: .contentProcessed { _ in
            connection.cancel()
        })
    }

    // MARK: - Send responses

    private func sendJSONRPCResult(_ connection: NWConnection, id: Int, result: String) {
        let body: [String: Any] = [
            "jsonrpc": "2.0",
            "id": id,
            "result": ["content": [["type": "text", "text": result]]],
        ]
        sendJSON(connection, body: body)
    }

    private func sendJSONRPCListResult(_ connection: NWConnection, id: Int, toolsJSON: String) {
        // tools/list expects result.tools array directly, not wrapped in content
        if let data = toolsJSON.data(using: .utf8),
           let toolsObj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            let body: [String: Any] = [
                "jsonrpc": "2.0",
                "id": id,
                "result": toolsObj,
            ]
            sendJSON(connection, body: body)
        } else {
            sendJSONRPCError(connection, id: id, code: -1, message: "internal error: tools encoding failed")
        }
    }

    private func sendJSONRPCError(_ connection: NWConnection, id: Int?, code: Int, message: String) {
        var body: [String: Any] = [
            "jsonrpc": "2.0",
            "error": ["code": code, "message": message],
        ]
        if let id = id {
            body["id"] = id
        } else {
            body["id"] = NSNull()
        }
        sendJSON(connection, body: body)
    }

    private func sendJSON(_ connection: NWConnection, body: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: body) else { return }
        let response = HTTPResponse(
            status: 200,
            headers: [
                ("Content-Type", "application/json"),
                ("Connection", "keep-alive"),
                ("Access-Control-Allow-Origin", "*"),
                ("Access-Control-Allow-Methods", "POST, GET, OPTIONS"),
                ("Access-Control-Allow-Headers", "Content-Type"),
            ],
            body: data
        )
        sendResponse(connection, response)
    }

    // MARK: - HTTP serialization

    private func sendResponse(_ connection: NWConnection, _ response: HTTPResponse) {
        var header = "HTTP/1.1 \(response.status) \(statusText(response.status))\r\n"
        header += "Content-Length: \(response.body.count)\r\n"
        for (key, value) in response.headers {
            header += "\(key): \(value)\r\n"
        }
        header += "\r\n"

        var data = header.data(using: .utf8) ?? Data()
        data.append(response.body)

        connection.send(content: data, completion: .contentProcessed { _ in
            if response.headers.contains(where: { $0.0 == "Connection" && $0.1 == "keep-alive" }) {
                // Keep alive for SSE
            } else {
                connection.cancel()
            }
        })
    }

    private func statusText(_ code: Int) -> String {
        switch code {
        case 200: return "OK"
        case 400: return "Bad Request"
        case 404: return "Not Found"
        case 500: return "Internal Server Error"
        default: return "Unknown"
        }
    }

    // MARK: - HTTP parser

    private func parseHTTPRequest(_ data: Data) -> HTTPRequest? {
        guard let crlfcrlf = data.firstRange(of: "\r\n\r\n".data(using: .utf8)!) else {
            return nil
        }

        let head = data[..<crlfcrlf.startIndex]
        let body = data[crlfcrlf.endIndex...]

        guard let headStr = String(data: head, encoding: .utf8) else { return nil }
        var lines = headStr.components(separatedBy: "\r\n")

        guard !lines.isEmpty else { return nil }
        let requestLine = lines.removeFirst()
        let parts = requestLine.components(separatedBy: " ")
        guard parts.count >= 2 else { return nil }

        let method = parts[0]
        let path = parts[1]

        var headers: [String: String] = [:]
        for line in lines {
            if let colon = line.firstIndex(of: ":") {
                let key = String(line[..<colon]).trimmingCharacters(in: .whitespaces)
                let value = String(line[line.index(after: colon)...]).trimmingCharacters(in: .whitespaces)
                headers[key.lowercased()] = value
            }
        }

        // Read exact body length if Content-Length is specified
        let requestBody = Data(body)
        if let contentLengthStr = headers["content-length"],
           let contentLength = Int(contentLengthStr),
           body.count < contentLength {
            // Wait for more data — shouldn't happen with receive(maximumLength:) but handle gracefully
            // For now, just use what we have
        }

        return HTTPRequest(method: method, path: path, headers: headers, body: requestBody)
    }
}

// MARK: - Thread-safe dictionary for SSE connections

final class SSEManager: @unchecked Sendable {
    private let lock = NSLock()
    private var connections: [UUID: NWConnection] = [:]

    subscript(id: UUID) -> NWConnection? {
        get { lock.withLock { connections[id] } }
        set { lock.withLock { connections[id] = newValue } }
    }

    func removeValue(forKey id: UUID) {
        lock.withLock { _ = connections.removeValue(forKey: id) }
    }

    var all: [UUID: NWConnection] {
        lock.withLock { connections }
    }

    func removeAll() {
        lock.withLock { connections.removeAll() }
    }
}

// MARK: - Task management for async tools/call

enum ManagedTaskState: Equatable {
    case pending
    case running
    case completed(String)
    case failed(Int, String)
}

struct ManagedTask {
    let id: String
    let name: String
    let arguments: [String: JSONValue]
    let rpcId: Int
    var state: ManagedTaskState
}

final class TaskManager: @unchecked Sendable {
    private let lock = NSLock()
    private var tasks: [String: ManagedTask] = [:]
    private var queue: [String] = []

    func add(id: String, name: String, args: [String: JSONValue], rpcId: Int) {
        lock.withLock {
            tasks[id] = ManagedTask(id: id, name: name, arguments: args, rpcId: rpcId, state: .pending)
            queue.append(id)
        }
    }

    func get(id: String) -> ManagedTask? {
        lock.withLock { tasks[id] }
    }

    func update(id: String, state: ManagedTaskState) {
        lock.withLock { tasks[id]?.state = state }
    }

    func nextPending() -> String? {
        lock.withLock { queue.first { tasks[$0]?.state == .pending } }
    }
}
