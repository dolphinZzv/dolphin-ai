import Foundation
import NIO
import NIOHTTP1

class HttpPipelineHandler: ChannelInboundHandler {
    typealias InboundIn = HTTPServerRequestPart

    let server: McpServer

    init(server: McpServer) {
        self.server = server
    }

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        let req = self.unwrapInboundIn(data)
        switch req {
        case .head(let head):
            handleRequest(head: head, context: context)
        case .body:
            break
        case .end:
            break
        }
    }

    private func handleRequest(head: HTTPServerRequestHead, context: ChannelHandlerContext) {
        let uri = head.uri
        let method = head.method

        if uri == "/health" && method == .GET {
            sendResponse(context: context, status: .ok, body: "{\"status\":\"ok\"}")
            return
        }

        if uri == "/mcp/call" && method == .POST {
            handleCall(context: context)
            return
        }

        if uri.hasPrefix("/mcp/stream") && method == .GET {
            handleStream(uri: uri, context: context)
            return
        }

        sendResponse(context: context, status: .notFound, body: "{\"error\":\"not found\"}")
    }

    private func handleCall(context: ChannelHandlerContext) {
        var accumulated = ByteBuffer()
        context.channel.read { result in
            if case .some(.byteBuffer(let buf)) = result {
                accumulated.writeBuffer(buf)
                context.channel.read(self.channelRead(context:data:))
            } else if case .some(.end) = result {
                guard let body = String(data: Data(accumulated.readBytes(accumulated.readableBytes) ?? []), encoding: .utf8) else {
                    self.sendResponse(context: context, status: .badRequest, body: "{\"error\":\"invalid body\"}")
                    return
                }
                Task {
                    let request = try? JSONDecoder().decode(JsonRpcRequest.self, from: body.data(using: .utf8)!)
                    if let req = request {
                        let response = await self.server.handle(request: req)
                        let responseData = try! JSONEncoder().encode(response)
                        let responseStr = String(data: responseData, encoding: .utf8)!
                        self.sendResponse(context: context, status: .ok, body: responseStr)
                    } else {
                        self.sendResponse(context: context, status: .badRequest, body: "{\"error\":\"invalid json\"}")
                    }
                }
            } else {
                self.channelRead(context: data)
            }
        }
    }

    private func handleStream(uri: URI, context: ChannelHandlerContext) {
        let query = uri.query ?? ""
        var since: Int64 = 0
        var sessionId: String?

        let params = query.split(separator: "&")
        for param in params {
            let keyValue = param.split(separator: "=")
            if keyValue.count == 2 {
                let key = String(keyValue[0])
                let value = String(keyValue[1])
                if key == "since" {
                    since = Int64(value) ?? 0
                } else if key == "sessionId" {
                    sessionId = value
                }
            }
        }

        guard let sid = sessionId else {
            sendResponse(context: context, status: .badRequest, body: "{\"error\":\"missing sessionId\"}")
            return
        }

        server.lock.lock()
        let session = server.sessions[sid]
        server.lock.unlock()

        guard let sess = session else {
            sendResponse(context: context, status: .notFound, body: "{\"error\":\"session not found\"}")
            return
        }

        let events = sess.getEvents(since: since)
        var response = ""
        for event in events {
            response += event.toJson()
        }

        sendResponse(context: context, status: .ok, body: response)
    }

    private func sendResponse(context: ChannelHandlerContext, status: HTTPResponseStatus, body: String) {
        let bodyData = body.data(using: .utf8) ?? Data()
        var buffer = ByteBuffer()
        buffer.writeBytes(bodyData)

        let head = HTTPResponseHead(
            status: status,
            headers: HTTPHeaders([("content-type", "application/json"), ("content-length", "\(bodyData.count)")])
        )
        context.write(self.wrapOutboundOut(HTTPServerResponsePart.head(head)))
        context.write(self.wrapOutboundOut(HTTPServerResponsePart.body(.bytes(buffer))))
        context.write(self.wrapOutboundOut(HTTPServerResponsePart.end(nil)))
    }
}