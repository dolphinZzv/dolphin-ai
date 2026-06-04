import XCTest
@testable import BrowserMCPCore

final class MCPProtocolTests: XCTestCase {

    // MARK: - Tool definitions

    func testToolCount() {
        let tools = TestToolHandler.tools()
        XCTAssertEqual(tools.count, 9)
    }

    func testToolNames() {
        let tools = TestToolHandler.tools()
        let names = tools.map(\.name).sorted()
        XCTAssertEqual(names, [
            "browser_activate_tab",
            "browser_close_tab",
            "browser_create_tab",
            "browser_evaluate",
            "browser_list_tabs",
            "browser_navigate",
            "browser_screenshot",
            "browser_set_window_size",
            "browser_wait",
        ])
    }

    func testToolNavigateSchema() {
        let tools = TestToolHandler.tools()
        let nav = tools.first { $0.name == "browser_navigate" }
        XCTAssertNotNil(nav)
        XCTAssertTrue(nav!.inputSchema.required?.contains("url") ?? false)
        // tab_id is optional
        XCTAssertNil(nav!.inputSchema.properties["tab_id"]?.default)
        XCTAssertEqual(nav!.inputSchema.properties["tab_id"]?.type, "string")
    }

    func testToolEvaluateSchema() {
        let tools = TestToolHandler.tools()
        let eval = tools.first { $0.name == "browser_evaluate" }
        XCTAssertNotNil(eval)
        XCTAssertTrue(eval!.inputSchema.required?.contains("expression") ?? false)
        // tab_id is optional
        XCTAssertEqual(eval!.inputSchema.properties["tab_id"]?.type, "string")
    }

    func testToolScreenshotSchema() {
        let tools = TestToolHandler.tools()
        let ss = tools.first { $0.name == "browser_screenshot" }
        XCTAssertNotNil(ss)
        // Screenshot has optional params only
        XCTAssertNil(ss!.inputSchema.required)
        // tab_id is optional
        XCTAssertEqual(ss!.inputSchema.properties["tab_id"]?.type, "string")
    }

    func testWindowSizeSchema() {
        let tools = TestToolHandler.tools()
        let ws = tools.first { $0.name == "browser_set_window_size" }
        XCTAssertNotNil(ws)
        XCTAssertTrue(ws!.inputSchema.required?.contains("width") ?? false)
        XCTAssertTrue(ws!.inputSchema.required?.contains("height") ?? false)
    }

    // MARK: - Tab tool schema tests

    func testCreateTabSchema() {
        let tools = TestToolHandler.tools()
        let tool = tools.first { $0.name == "browser_create_tab" }
        XCTAssertNotNil(tool)
        XCTAssertNil(tool!.inputSchema.required)
        XCTAssertTrue(tool!.inputSchema.properties.isEmpty)
    }

    func testCloseTabSchema() {
        let tools = TestToolHandler.tools()
        let tool = tools.first { $0.name == "browser_close_tab" }
        XCTAssertNotNil(tool)
        XCTAssertTrue(tool!.inputSchema.required?.contains("tab_id") ?? false)
        XCTAssertEqual(tool!.inputSchema.properties["tab_id"]?.type, "string")
    }

    func testListTabsSchema() {
        let tools = TestToolHandler.tools()
        let tool = tools.first { $0.name == "browser_list_tabs" }
        XCTAssertNotNil(tool)
        XCTAssertNil(tool!.inputSchema.required)
        XCTAssertTrue(tool!.inputSchema.properties.isEmpty)
    }

    func testActivateTabSchema() {
        let tools = TestToolHandler.tools()
        let tool = tools.first { $0.name == "browser_activate_tab" }
        XCTAssertNotNil(tool)
        XCTAssertTrue(tool!.inputSchema.required?.contains("tab_id") ?? false)
        XCTAssertEqual(tool!.inputSchema.properties["tab_id"]?.type, "string")
    }

    func testWaitSchema() {
        let tools = TestToolHandler.tools()
        let tool = tools.first { $0.name == "browser_wait" }
        XCTAssertNotNil(tool)
        XCTAssertTrue(tool!.inputSchema.required?.contains("selector") ?? false)
        XCTAssertEqual(tool!.inputSchema.properties["selector"]?.type, "string")
        XCTAssertEqual(tool!.inputSchema.properties["state"]?.type, "string")
        XCTAssertEqual(tool!.inputSchema.properties["timeout"]?.type, "number")
        XCTAssertEqual(tool!.inputSchema.properties["tab_id"]?.type, "string")
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

    func testValidateTabTools() {
        // browser_create_tab: no required params -> always valid
        XCTAssertEqual(TestToolHandler.validateCall("browser_create_tab", args: [:]), true)
        // browser_list_tabs: no required params -> always valid
        XCTAssertEqual(TestToolHandler.validateCall("browser_list_tabs", args: [:]), true)

        // browser_close_tab: tab_id required
        XCTAssertEqual(TestToolHandler.validateCall("browser_close_tab", args: [:]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_close_tab", args: ["tab_id": .string("")]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_close_tab", args: ["tab_id": .string("tab-1")]), true)

        // browser_activate_tab: tab_id required
        XCTAssertEqual(TestToolHandler.validateCall("browser_activate_tab", args: [:]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_activate_tab", args: ["tab_id": .string("")]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_activate_tab", args: ["tab_id": .string("tab-1")]), true)

        // browser_wait: selector required, state/timeout/tab_id optional
        XCTAssertEqual(TestToolHandler.validateCall("browser_wait", args: [:]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_wait", args: ["selector": .string("")]), false)
        XCTAssertEqual(TestToolHandler.validateCall("browser_wait", args: ["selector": .string(".my-class")]), true)
        XCTAssertEqual(TestToolHandler.validateCall("browser_wait", args: ["selector": .string("#id"), "state": .string("visible")]), true)
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
