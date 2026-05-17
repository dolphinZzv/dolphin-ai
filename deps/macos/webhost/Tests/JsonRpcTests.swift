import XCTest
@testable import WebHost

final class JsonRpcRequestTests: XCTestCase {
    func testRequestParsing() throws {
        let json = """
        {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "tools/call",
            "params": {
                "name": "web_session_create",
                "arguments": {
                    "viewport": {"width": 1920, "height": 1080}
                }
            }
        }
        """

        let data = json.data(using: .utf8)!
        let request = try JSONDecoder().decode(JsonRpcRequest.self, from: data)

        XCTAssertEqual(request.jsonrpc, "2.0")
        XCTAssertEqual(request.id as? Int, 1)
        XCTAssertEqual(request.method, "tools/call")
        XCTAssertEqual(request.params?.name, "web_session_create")
    }

    func testRequestWithNoParams() throws {
        let json = """
        {
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tools/list"
        }
        """

        let data = json.data(using: .utf8)!
        let request = try JSONDecoder().decode(JsonRpcRequest.self, from: data)

        XCTAssertEqual(request.jsonrpc, "2.0")
        XCTAssertEqual(request.id as? Int, 2)
        XCTAssertEqual(request.method, "tools/list")
        XCTAssertNil(request.params)
    }
}

final class JsonRpcResponseTests: XCTestCase {
    func testSuccessResponse() {
        let result = JsonRpcResult(success: true, sessionId: "sess_001")
        let response = JsonRpcResponse(id: 1, result: result)

        XCTAssertEqual(response.jsonrpc, "2.0")
        XCTAssertEqual(response.id as? Int, 1)
        XCTAssertTrue(response.result?.success ?? false)
        XCTAssertEqual(response.result?.sessionId, "sess_001")
        XCTAssertNil(response.error)
    }

    func testErrorResponse() {
        let response = JsonRpcResponse(id: 1, error: .sessionNotFound)

        XCTAssertEqual(response.jsonrpc, "2.0")
        XCTAssertEqual(response.id as? Int, 1)
        XCTAssertNil(response.result)
        XCTAssertEqual(response.error?.code, -32000)
        XCTAssertEqual(response.error?.message, "Session not found")
    }
}

final class JsonRpcParamsTests: XCTestCase {
    func testStringArg() throws {
        let params = JsonRpcParams(name: "page_open", arguments: [
            "sessionId": AnyCodable("sess_001"),
            "url": AnyCodable("https://example.com")
        ])

        XCTAssertEqual(params.stringArg("sessionId"), "sess_001")
        XCTAssertEqual(params.stringArg("url"), "https://example.com")
        XCTAssertNil(params.stringArg("nonexistent"))
    }

    func testIntArg() throws {
        let params = JsonRpcParams(name: "script_run", arguments: [
            "timeout": AnyCodable(5000)
        ])

        XCTAssertEqual(params.intArg("timeout"), 5000)
        XCTAssertNil(params.intArg("nonexistent"))
    }

    func testBoolArg() throws {
        let params = JsonRpcParams(name: "web_set_interactive", arguments: [
            "interactive": AnyCodable(true)
        ])

        XCTAssertEqual(params.boolArg("interactive"), true)
        XCTAssertNil(params.boolArg("nonexistent"))
    }
}

final class JsonRpcErrorTests: XCTestCase {
    func testErrorCodes() {
        XCTAssertEqual(JsonRpcError.parseError.code, -32700)
        XCTAssertEqual(JsonRpcError.methodNotFound.code, -32601)
        XCTAssertEqual(JsonRpcError.invalidParams.code, -32602)
        XCTAssertEqual(JsonRpcError.internalError.code, -32603)
        XCTAssertEqual(JsonRpcError.sessionNotFound.code, -32000)
        XCTAssertEqual(JsonRpcError.sessionLimitExceeded.code, -32001)
        XCTAssertEqual(JsonRpcError.navigationTimeout.code, -32002)
        XCTAssertEqual(JsonRpcError.scriptTimeout.code, -32003)
    }

    func testErrorMessages() {
        XCTAssertEqual(JsonRpcError.sessionNotFound.message, "Session not found")
        XCTAssertEqual(JsonRpcError.navigationTimeout.message, "Navigation timeout")
    }
}