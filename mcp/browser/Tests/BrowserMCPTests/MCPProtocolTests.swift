import XCTest
@testable import BrowserMCPCore

final class MCPProtocolTests: XCTestCase {

    // MARK: - Tool definitions

    func testToolCount() {
        let tools = TestToolHandler.tools()
        XCTAssertEqual(tools.count, 4)
    }

    func testToolNames() {
        let tools = TestToolHandler.tools()
        let names = tools.map(\.name).sorted()
        XCTAssertEqual(names, ["browser_evaluate", "browser_navigate", "browser_screenshot", "browser_set_window_size"])
    }

    func testToolNavigateSchema() {
        let tools = TestToolHandler.tools()
        let nav = tools.first { $0.name == "browser_navigate" }
        XCTAssertNotNil(nav)
        XCTAssertTrue(nav!.inputSchema.required?.contains("url") ?? false)
    }

    func testToolEvaluateSchema() {
        let tools = TestToolHandler.tools()
        let eval = tools.first { $0.name == "browser_evaluate" }
        XCTAssertNotNil(eval)
        XCTAssertTrue(eval!.inputSchema.required?.contains("expression") ?? false)
    }

    func testToolScreenshotSchema() {
        let tools = TestToolHandler.tools()
        let ss = tools.first { $0.name == "browser_screenshot" }
        XCTAssertNotNil(ss)
        // Screenshot has optional params only
        XCTAssertNil(ss!.inputSchema.required)
    }

    func testWindowSizeSchema() {
        let tools = TestToolHandler.tools()
        let ws = tools.first { $0.name == "browser_set_window_size" }
        XCTAssertNotNil(ws)
        XCTAssertTrue(ws!.inputSchema.required?.contains("width") ?? false)
        XCTAssertTrue(ws!.inputSchema.required?.contains("height") ?? false)
    }

    // MARK: - Validation logic

    func testValidateURL() {
        XCTAssertEqual(TestToolHandler.validateCall("browser_navigate", args: [:]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_navigate", args: ["url": .string("")]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_navigate", args: ["url": .string("https://example.com")]), true)
    }

    func testValidateExpression() {
        XCTAssertEqual(TestToolHandler.validateCall("browser_evaluate", args: [:]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_evaluate", args: ["expression": .string("")]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_evaluate", args: ["expression": .string("1+1")]), true)
    }

    func testValidateUnknownTool() {
        XCTAssertNil(TestToolHandler.validateCall("browser_unknown", args: [:]))
    }

    // MARK: - Tool definition descriptions

    func testToolDescriptionsNotEmpty() {
        for tool in TestToolHandler.tools() {
            XCTAssertFalse(tool.description.isEmpty, "\(tool.name) has empty description")
        }
    }
}
