import XCTest

// MARK: - Sendable helper for stdout collection

final class StdoutBuffer: @unchecked Sendable {
    var data = Data()
    func append(_ more: Data) { data.append(more) }

    /// Remove and return the data up to the newline, advancing past it.
    func consumeLine(upTo nlIndex: Data.Index) -> Data {
        let line = data[..<nlIndex]
        data = data[(nlIndex + 1)...]
        return Data(line)
    }
}

// MARK: - MCP integration test (requires built BrowserMCP app)

/// Integration test that launches the BrowserMCP process and communicates
/// via JSON-RPC over stdin/stdout.
///
/// To run: `make browser && swift test --filter BrowserMCPIntegrationTests`
final class BrowserMCPIntegrationTests: XCTestCase {
    var process: Process!
    var stdinPipe: Pipe!
    var stdoutPipe: Pipe!
    var stdoutBuffer: StdoutBuffer!
    var tempScreenshotDir: String!

    override func setUpWithError() throws {
        // Locate the BrowserMCP executable
        guard let bin = locateBinary() else {
            throw XCTSkip("BrowserMCP binary not found. Run 'make browser' first.")
        }

        let testDir = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent() // Tests/BrowserMCPTests/
        tempScreenshotDir = testDir.appendingPathComponent("screenshots").path
        try FileManager.default.createDirectory(atPath: tempScreenshotDir, withIntermediateDirectories: true)

        process = Process()
        stdinPipe = Pipe()
        stdoutPipe = Pipe()
        stdoutBuffer = StdoutBuffer()

        process.executableURL = bin
        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe

        // Collect stdout data
        let buffer = stdoutBuffer!
        stdoutPipe.fileHandleForReading.readabilityHandler = { handle in
            buffer.append(handle.availableData)
        }

        try process.run()
    }

    override func tearDownWithError() throws {
        stdoutPipe.fileHandleForReading.readabilityHandler = nil
        process?.terminate()
        process = nil
        stdinPipe = nil
        stdoutPipe = nil
        stdoutBuffer = nil

        // Clean up screenshots directory
        if let dir = tempScreenshotDir {
            try? FileManager.default.removeItem(atPath: dir)
            tempScreenshotDir = nil
        }
    }

    // MARK: - Tests

    func testNavigateToBingAndScreenshot() throws {
        // Step 1: Navigate to bing.com
        let navCmd = mcpCall(name: "browser_navigate", args: ["url": "https://www.bing.com"])
        write(navCmd)

        // Wait for navigation response (up to 35s)
        guard let navResp = waitForResponse(timeout: 35) else {
            XCTFail("No response for navigate command")
            return
        }
        XCTAssertNotNil(navResp["result"], "navigate should succeed: \(navResp)")

        // Step 2: Take screenshot
        let ssCmd = mcpCall(name: "browser_screenshot", args: ["output_dir": tempScreenshotDir!])
        write(ssCmd)

        guard let ssResp = waitForResponse(timeout: 20) else {
            XCTFail("No response for screenshot command")
            return
        }
        XCTAssertNotNil(ssResp["result"], "screenshot should succeed: \(ssResp)")

        // Step 3: Verify screenshot file exists
        if let result = ssResp["result"] as? [String: Any],
           let content = result["content"] as? [[String: Any]],
           let text = content.first?["text"] as? String,
           let data = text.data(using: .utf8),
           let json = try? JSONSerialization.jsonObject(with: data) as? [String: String],
           let path = json["path"] {
            XCTAssertTrue(FileManager.default.fileExists(atPath: path), "Screenshot file should exist at: \(path)")
            let attrs = try FileManager.default.attributesOfItem(atPath: path)
            let size = attrs[.size] as? Int ?? 0
            XCTAssertGreaterThan(size, 1000, "Screenshot should be larger than 1KB")
        } else {
            XCTFail("Could not extract screenshot path from response: \(ssResp)")
        }
    }

    func testScreenshotOutputDir() throws {
        // First navigate to a page
        let navCmd = mcpCall(name: "browser_navigate", args: ["url": "https://www.bing.com"])
        write(navCmd)
        _ = waitForResponse(timeout: 35)

        // Then screenshot with custom output dir
        let ssCmd = mcpCall(name: "browser_screenshot", args: ["output_dir": tempScreenshotDir!])
        write(ssCmd)

        guard let resp = waitForResponse(timeout: 20) else {
            XCTFail("No response for screenshot command")
            return
        }
        XCTAssertNotNil(resp["result"], "screenshot should return result: \(resp)")
    }

    // MARK: - Helpers

    private func mcpCall(name: String, args: [String: String]) -> Data {
        let body: [String: Any] = [
            "jsonrpc": "2.0",
            "id": Int.random(in: 1...9999),
            "method": "tools/call",
            "params": [
                "name": name,
                "arguments": args,
            ],
        ]
        return try! JSONSerialization.data(withJSONObject: body) + "\n".data(using: .utf8)!
    }

    private func write(_ data: Data) {
        stdinPipe.fileHandleForWriting.write(data)
    }

    private func waitForResponse(timeout: TimeInterval) -> [String: Any]? {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if let resp = tryParseJSON() {
                return resp
            }
            Thread.sleep(forTimeInterval: 0.05)
        }
        return tryParseJSON()
    }

    private func tryParseJSON() -> [String: Any]? {
        let current = stdoutBuffer.data
        guard !current.isEmpty else { return nil }
        // Find first complete JSON object by scanning for newlines
        guard let nlIndex = current.firstIndex(of: 0x0A) else { return nil }

        let line = stdoutBuffer.consumeLine(upTo: nlIndex)

        guard let obj = try? JSONSerialization.jsonObject(with: line) as? [String: Any] else {
            return nil
        }
        return obj
    }

    private func locateBinary() -> URL? {
        // Check several locations
        let candidates: [String] = [
            ".build/release/BrowserMCP",
            ".build/debug/BrowserMCP",
            "bin/BrowserMCP.app/Contents/MacOS/BrowserMCP",
            "../../bin/BrowserMCP.app/Contents/MacOS/BrowserMCP",
            "../../bin/cdp-server",
        ]

        let base = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent() // Tests/BrowserMCPTests/
            .deletingLastPathComponent() // Tests/
            .deletingLastPathComponent() // browser/

        for candidate in candidates {
            let url = base.appendingPathComponent(candidate)
            if FileManager.default.fileExists(atPath: url.path) {
                return url
            }
        }
        return nil
    }
}
