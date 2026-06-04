import XCTest
@testable import BrowserMCPCore

// MARK: - Test helper for tool definitions

/// Standalone validator that mirrors MCPServer's tool validation logic.
enum TestToolHandler {
    typealias ToolDef = (name: String, description: String, inputSchema: InputSchema)

    static func tools() -> [ToolDef] {
        [
            ToolDef(
                name: "browser_navigate",
                description: "Navigate to a URL in the specified tab (or active tab if not specified)",
                inputSchema: InputSchema(
                    properties: [
                        "url": PropertySchema(type: "string", description: "Target URL", default: nil),
                        "tab_id": PropertySchema(type: "string", description: "Tab ID (optional, defaults to active tab)", default: nil),
                    ],
                    required: ["url"]
                )
            ),
            ToolDef(
                name: "browser_evaluate",
                description: "Execute JavaScript on the specified page (or active tab) and return the result",
                inputSchema: InputSchema(
                    properties: [
                        "expression": PropertySchema(type: "string", description: "JavaScript expression to evaluate", default: nil),
                        "tab_id": PropertySchema(type: "string", description: "Tab ID (optional, defaults to active tab)", default: nil),
                    ],
                    required: ["expression"]
                )
            ),
            ToolDef(
                name: "browser_screenshot",
                description: "Take a screenshot and save to a file, return the file path",
                inputSchema: InputSchema(
                    properties: [
                        "url": PropertySchema(type: "string", description: "Optional URL to navigate to first", default: nil),
                        "output_dir": PropertySchema(type: "string", description: "Screenshot output directory, default: screenshots", default: nil),
                        "tab_id": PropertySchema(type: "string", description: "Tab ID (optional, defaults to active tab)", default: nil),
                    ],
                    required: nil
                )
            ),
            ToolDef(
                name: "browser_set_window_size",
                description: "Resize the browser window to the given width and height in pixels",
                inputSchema: InputSchema(
                    properties: [
                        "width": PropertySchema(type: "number", description: "Window width in pixels, min 400", default: nil),
                        "height": PropertySchema(type: "number", description: "Window height in pixels, min 300", default: nil),
                    ],
                    required: ["width", "height"]
                )
            ),
            ToolDef(
                name: "browser_create_tab",
                description: "Create a new browser tab and return its tab_id",
                inputSchema: InputSchema(
                    properties: [:],
                    required: nil
                )
            ),
            ToolDef(
                name: "browser_close_tab",
                description: "Close a browser tab by tab_id. At least one tab must remain open.",
                inputSchema: InputSchema(
                    properties: ["tab_id": PropertySchema(type: "string", description: "Tab ID to close", default: nil)],
                    required: ["tab_id"]
                )
            ),
            ToolDef(
                name: "browser_list_tabs",
                description: "List all open browser tabs with their IDs, URLs, and titles",
                inputSchema: InputSchema(
                    properties: [:],
                    required: nil
                )
            ),
            ToolDef(
                name: "browser_activate_tab",
                description: "Switch the active tab by tab_id. This brings the tab into view.",
                inputSchema: InputSchema(
                    properties: ["tab_id": PropertySchema(type: "string", description: "Tab ID to activate", default: nil)],
                    required: ["tab_id"]
                )
            ),
            ToolDef(
                name: "browser_wait",
                description: "Wait for a condition on the page (element exists/visible/gone, or DOM stable). Returns true when condition is met, false on timeout.",
                inputSchema: InputSchema(
                    properties: [
                        "selector": PropertySchema(type: "string", description: "CSS selector to watch", default: nil),
                        "state": PropertySchema(type: "string", description: "Wait condition: exists (default), visible, gone, or stable", default: nil),
                        "timeout": PropertySchema(type: "number", description: "Timeout in seconds (default 10)", default: nil),
                        "tab_id": PropertySchema(type: "string", description: "Tab ID (optional, defaults to active tab)", default: nil),
                    ],
                    required: ["selector"]
                )
            ),
        ]
    }

    /// Returns non-nil result if the call is valid (passes schema validation).
    static func validateCall(_ name: String, args: [String: JSONValue]) -> Bool? {
        guard let tool = tools().first(where: { $0.name == name }) else {
            return nil // unknown tool
        }
        if let required = tool.inputSchema.required {
            for key in required {
                if let val = args[key] {
                    if case .string(let s) = val, s.isEmpty {
                        return false
                    }
                } else {
                    return false
                }
            }
        }
        return true
    }
}
