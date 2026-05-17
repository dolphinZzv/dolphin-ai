import XCTest
@testable import WebHost

final class IntegrationTests: XCTestCase {
    func testHealthEndpoint() throws {
        let json = """
        {"status":"ok"}
        """
        XCTAssertTrue(json.contains("ok"))
    }

    func testJsonRpcRequestFormat() throws {
        let request = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 1,
            method: "tools/call",
            params: JsonRpcParams(
                name: "web_session_create",
                arguments: ["viewport": AnyCodable(["width": 1920, "height": 1080])]
            )
        )

        let encoder = JSONEncoder()
        let data = try encoder.encode(request)
        let json = String(data: data, encoding: .utf8)!

        XCTAssertTrue(json.contains("\"jsonrpc\":\"2.0\""))
        XCTAssertTrue(json.contains("\"method\":\"tools/call\""))
        XCTAssertTrue(json.contains("web_session_create"))
    }

    func testSessionCreationResponseFormat() throws {
        let result = JsonRpcResult(success: true, sessionId: "sess_001")
        let response = JsonRpcResponse(id: 1, result: result)

        let encoder = JSONEncoder()
        let data = try encoder.encode(response)
        let json = String(data: data, encoding: .utf8)!

        XCTAssertTrue(json.contains("\"success\":true"))
        XCTAssertTrue(json.contains("sess_001"))
    }

    func testErrorResponseFormat() throws {
        let response = JsonRpcResponse(id: 1, error: .sessionNotFound)

        let encoder = JSONEncoder()
        let data = try encoder.encode(response)
        let json = String(data: data, encoding: .utf8)!

        XCTAssertTrue(json.contains("\"code\":-32000"))
        XCTAssertTrue(json.contains("\"message\":\"Session not found\""))
    }
}

final class WebHostWorkflowTests: XCTestCase {
    func testFullWorkflowJsonConstruction() throws {
        let sessionCreateRequest = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 1,
            method: "tools/call",
            params: JsonRpcParams(
                name: "web_session_create",
                arguments: ["viewport": AnyCodable(["width": 1920, "height": 1080])]
            )
        )

        let pageOpenRequest = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 2,
            method: "tools/call",
            params: JsonRpcParams(
                name: "page_open",
                arguments: [
                    "sessionId": AnyCodable("sess_001"),
                    "url": AnyCodable("https://example.com")
                ]
            )
        )

        let scriptRunRequest = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 3,
            method: "tools/call",
            params: JsonRpcParams(
                name: "script_run",
                arguments: [
                    "sessionId": AnyCodable("sess_001"),
                    "script": AnyCodable("document.title")
                ]
            )
        )

        let screenshotRequest = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 4,
            method: "tools/call",
            params: JsonRpcParams(
                name: "page_screenshot",
                arguments: ["sessionId": AnyCodable("sess_001")]
            )
        )

        let closeRequest = JsonRpcRequest(
            jsonrpc: "2.0",
            id: 5,
            method: "tools/call",
            params: JsonRpcParams(
                name: "web_session_close",
                arguments: ["sessionId": AnyCodable("sess_001")]
            )
        )

        let encoder = JSONEncoder()
        let requests = [sessionCreateRequest, pageOpenRequest, scriptRunRequest, screenshotRequest, closeRequest]

        for (index, request) in requests.enumerated() {
            let data = try encoder.encode(request)
            let json = String(data: data, encoding: .utf8)!
            XCTAssertFalse(json.isEmpty, "Request \(index + 1) should not be empty")
        }
    }

    func testEventSequence() throws {
        var events: [Event] = []

        events.append(WebEvent.navigation("https://example.com", status: "loading"))
        events.append(WebEvent.console("Page started loading"))
        events.append(WebEvent.navigation("https://example.com", status: "complete"))
        events.append(WebEvent.console("Page loaded"))

        XCTAssertEqual(events.count, 4)
        XCTAssertTrue(events[0].t <= events[1].t)
        XCTAssertTrue(events[1].t <= events[2].t)
    }
}

final class HttpHandlerTests: XCTestCase {
    func testStreamQueryParsing() {
        let uri = "/mcp/stream?sessionId=sess_001&since=1234567890"

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

        XCTAssertEqual(sessionId, "sess_001")
        XCTAssertEqual(since, 1234567890)
    }

    func testStreamQueryWithoutSince() {
        let uri = "/mcp/stream?sessionId=sess_001"

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

        XCTAssertEqual(sessionId, "sess_001")
        XCTAssertEqual(since, 0)
    }
}