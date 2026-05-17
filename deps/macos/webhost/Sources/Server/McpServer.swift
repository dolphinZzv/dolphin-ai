import Foundation
import NIO

class McpServer {
    let eventLoopGroup: MultiThreadedEventLoopGroup
    var sessions: [String: WebKitSession] = [:]
    let sessionManager: SessionManager
    let lock = NSLock()

    init(eventLoopGroup: MultiThreadedEventLoopGroup) {
        self.eventLoopGroup = eventLoopGroup
        self.sessionManager = SessionManager()
    }

    func handle(request: JsonRpcRequest) async -> JsonRpcResponse {
        guard request.method == "tools/call",
              let params = request.params,
              let toolName = params.name else {
            return JsonRpcResponse(id: request.id, error: .invalidParams)
        }

        let arguments = params.arguments

        switch toolName {
        case "web_session_create":
            return await createSession(arguments: arguments, requestId: request.id)
        case "page_open":
            return await navigate(arguments: arguments, requestId: request.id)
        case "script_run":
            return await evaluate(arguments: arguments, requestId: request.id)
        case "page_screenshot":
            return await screenshot(arguments: arguments, requestId: request.id)
        case "web_set_interactive":
            return await setInteractive(arguments: arguments, requestId: request.id)
        case "web_capabilities":
            return await getCapabilities(arguments: arguments, requestId: request.id)
        case "web_session_close":
            return await closeSession(arguments: arguments, requestId: request.id)
        default:
            return JsonRpcResponse(id: request.id, error: .methodNotFound)
        }
    }

    private func createSession(arguments: [String: AnyCodable]?, requestId: Any?) async -> JsonRpcResponse {
        lock.lock()
        defer { lock.unlock() }

        let viewport = parseViewport(arguments)
        let sessionId = UUID().uuidString

        let session = WebKitSession(id: sessionId, viewport: viewport)

        sessions[sessionId] = session
        sessionManager.add(sessionId: sessionId)

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, sessionId: sessionId))
    }

    private func navigate(arguments: [String: AnyCodable]?, requestId: Any?) async -> JsonRpcResponse {
        guard let sessionId = arguments?.["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        guard let urlString = arguments?.["url"]?.value as? String,
              let url = URL(string: urlString) else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        session.navigate(to: url)

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(
            success: true,
            url: urlString,
            status: "loading"
        ))
    }

    private func evaluate(arguments: [String: AnyCodable]?, requestId: Any?) async -> JsonRpcResponse {
        guard let sessionId = arguments?.["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        guard let script = arguments?.["script"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        do {
            let result = try await session.evaluate(script: script)
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, value: result))
        } catch {
            return JsonRpcResponse(id: requestId, error: .internalError)
        }
    }

    private func screenshot(arguments: [String: AnyCodable]?, requestId: Any?) async -> JsonRpcResponse {
        guard let sessionId = arguments?.["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        do {
            let data = try await session.screenshot()
            let base64 = data.base64EncodedString()
            return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, data: base64))
        } catch {
            return JsonRpcResponse(id: requestId, error: .internalError)
        }
    }

    private func setInteractive(arguments: [String: AnyCodable]?, requestId: Any?) async -> JsonRpcResponse {
        guard let sessionId = arguments?.["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        let interactive = arguments?.["interactive"]?.value as? Bool ?? false

        lock.lock()
        let session = sessions[sessionId]
        lock.unlock()

        guard let session = session else {
            return JsonRpcResponse(id: requestId, error: .sessionNotFound)
        }

        session.setInteractive(interactive)

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true))
    }

    private func getCapabilities(arguments: [String: AnyCodable]?, requestId: Any?) async -> JsonRpcResponse {
        let capabilities = [
            "dialog": true,
            "popup": true,
            "screenshot": true,
            "fullPage": true,
            "console": true,
            "navigation": true,
            "upload": true,
            "download": false
        ]

        return JsonRpcResponse(id: requestId, result: JsonRpcResult(success: true, capabilities: capabilities))
    }

    private func closeSession(arguments: [String: AnyCodable]?, requestId: Any?) async -> JsonRpcResponse {
        guard let sessionId = arguments?.["sessionId"]?.value as? String else {
            return JsonRpcResponse(id: requestId, error: .invalidParams)
        }

        lock.lock()
        sessions.removeValue(forKey: sessionId)
        lock.unlock()

        sessionManager.remove(sessionId: sessionId)

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