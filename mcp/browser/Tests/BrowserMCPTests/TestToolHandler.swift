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
                description: "Navigate to a URL",
                inputSchema: InputSchema(
                    properties: ["url": PropertySchema(type: "string", description: "Target URL", default: nil)],
                    required: ["url"]
                )
            ),
            ToolDef(
                name: "browser_evaluate",
                description: "Execute JavaScript on the current page and return the result",
                inputSchema: InputSchema(
                    properties: ["expression": PropertySchema(type: "string", description: "JavaScript expression to evaluate", default: nil)],
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
