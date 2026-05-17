import XCTest
@testable import WebHost

final class McpServerTests: XCTestCase {
    var server: McpServer!

    override func setUp() {
        super.setUp()
        let group = MultiThreadedEventLoopGroup(numberOfThreads: 1)
        server = McpServer(eventLoopGroup: group)
    }

    override func tearDown() {
        try? server.eventLoopGroup.syncShutdownGracefully()
        super.tearDown()
    }

    func testCreateSession() async {
        let request = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 1,
            method: "tools/call",
            params: JsonRpcParams(
                name: "web_session_create",
                arguments: ["viewport": AnyCodable(["width": 1920, "height": 1080])]
            )
        )

        let response = await server.handle(request: request)

        XCTAssertTrue(response.result?.success ?? false)
        XCTAssertNotNil(response.result?.sessionId)
    }

    func testGetCapabilities() async {
        let request = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 2,
            method: "tools/call",
            params: JsonRpcParams(
                name: "web_capabilities",
                arguments: ["sessionId": AnyCodable("any-session")]
            )
        )

        let response = await server.handle(request: request)

        XCTAssertTrue(response.result?.success ?? false)
        XCTAssertNotNil(response.result?.capabilities)
        XCTAssertEqual(response.result?.capabilities?["screenshot"], true)
        XCTAssertEqual(response.result?.capabilities?["dialog"], true)
    }

    func testSessionNotFound() async {
        let request = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 3,
            method: "tools/call",
            params: JsonRpcParams(
                name: "page_open",
                arguments: [
                    "sessionId": AnyCodable("nonexistent-session"),
                    "url": AnyCodable("https://example.com")
                ]
            )
        )

        let response = await server.handle(request: request)

        XCTAssertNotNil(response.error)
        XCTAssertEqual(response.error?.code, -32000)
    }

    func testInvalidParams() async {
        let request = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 4,
            method: "tools/call",
            params: JsonRpcParams(
                name: "page_open",
                arguments: nil
            )
        )

        let response = await server.handle(request: request)

        XCTAssertNotNil(response.error)
        XCTAssertEqual(response.error?.code, -32602)
    }

    func testUnknownTool() async {
        let request = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 5,
            method: "tools/call",
            params: JsonRpcParams(
                name: "unknown_tool",
                arguments: nil
            )
        )

        let response = await server.handle(request: request)

        XCTAssertNotNil(response.error)
        XCTAssertEqual(response.error?.code, -32601)
    }
}

final class SessionManagerTests: XCTestCase {
    func testAddAndRemoveSession() {
        let manager = SessionManager()

        manager.add(sessionId: "sess_001")
        XCTAssertEqual(manager.count(), 1)

        manager.remove(sessionId: "sess_001")
        XCTAssertEqual(manager.count(), 0)
    }

    func testMultipleSessions() {
        let manager = SessionManager()

        manager.add(sessionId: "sess_001")
        manager.add(sessionId: "sess_002")
        manager.add(sessionId: "sess_003")

        XCTAssertEqual(manager.count(), 3)

        manager.remove(sessionId: "sess_002")
        XCTAssertEqual(manager.count(), 2)
    }

    func testRemoveNonexistent() {
        let manager = SessionManager()

        manager.add(sessionId: "sess_001")
        manager.remove(sessionId: "nonexistent")

        XCTAssertEqual(manager.count(), 1)
    }
}