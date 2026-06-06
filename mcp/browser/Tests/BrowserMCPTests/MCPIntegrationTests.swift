import XCTest

/// Integration test that launches the BrowserMCP process and communicates
/// via JSON-RPC over HTTP.
///
/// To run: `make browser && swift test --filter BrowserMCPIntegrationTests`
final class BrowserMCPIntegrationTests: XCTestCase {
    var process: Process!
    var baseURL: URL!
    var testDir: URL!

    override func setUpWithError() throws {
        guard let bin = locateBinary() else {
            throw XCTSkip("BrowserMCP binary not found. Run 'make browser' first.")
        }

        testDir = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent() // Tests/BrowserMCPTests/

        process = Process()
        process.executableURL = bin
        var env = ProcessInfo.processInfo.environment
        env["BROWSER_MCP_PORT"] = "19876"
        process.environment = env

        try process.run()

        baseURL = URL(string: "http://127.0.0.1:19876")!

        // Wait for the server to be ready
        try waitForServer(timeout: 10)
    }

    override func tearDownWithError() throws {
        process?.terminate()
        process = nil
        baseURL = nil
        testDir = nil
    }

    // MARK: - Tab E2E tests

    func testCreateTab() throws {
        let resp = try sendToolCall("browser_create_tab", args: [:])
        let result = try extractResult(resp)
        XCTAssertNotNil(result["tab_id"] as? String)
        XCTAssertEqual(result["status"] as? String, "ok")
    }

    func testListTabsInitiallyOne() throws {
        let resp = try sendToolCall("browser_list_tabs", args: [:])
        let result = try extractResult(resp)
        let tabs = result["tabs"] as? [[String: Any]]
        XCTAssertNotNil(tabs)
        XCTAssertEqual(tabs?.count, 1, "should start with one default tab")
    }

    func testCreateAndListMultipleTabs() throws {
        // Create 3 new tabs
        for _ in 0..<3 {
            let createResp = try sendToolCall("browser_create_tab", args: [:])
            let createResult = try extractResult(createResp)
            XCTAssertNotNil(createResult["tab_id"] as? String)
        }

        // List tabs — should have 4 now (1 default + 3 new)
        let listResp = try sendToolCall("browser_list_tabs", args: [:])
        let listResult = try extractResult(listResp)
        let tabs = listResult["tabs"] as? [[String: Any]]
        XCTAssertEqual(tabs?.count, 4)

        // One tab should be active
        let activeTabs = tabs?.filter { $0["is_active"] as? Bool == true }
        XCTAssertEqual(activeTabs?.count, 1)
    }

    func testCreateTabReturnsUniqueIDs() throws {
        var ids = Set<String>()

        for _ in 0..<3 {
            let resp = try sendToolCall("browser_create_tab", args: [:])
            let result = try extractResult(resp)
            let tabId = result["tab_id"] as? String
            XCTAssertNotNil(tabId)
            ids.insert(tabId!)
        }

        XCTAssertEqual(ids.count, 3, "each tab should have a unique id")
    }

    func testCloseTab() throws {
        // Create a tab
        let createResp = try sendToolCall("browser_create_tab", args: [:])
        let createResult = try extractResult(createResp)
        let tabId = createResult["tab_id"] as! String

        // Close it
        let closeResp = try sendToolCall("browser_close_tab", args: ["tab_id": tabId])
        let closeResult = try extractResult(closeResp)
        XCTAssertEqual(closeResult["status"] as? String, "ok")

        // List should have 1 tab again
        let listResp = try sendToolCall("browser_list_tabs", args: [:])
        let listResult = try extractResult(listResp)
        let tabs = listResult["tabs"] as? [[String: Any]]
        XCTAssertEqual(tabs?.count, 1)
    }

    func testCloseTabRemovesCorrectTab() throws {
        // Create 2 tabs
        let r1 = try extractResult(sendToolCall("browser_create_tab", args: [:]))
        let id1 = r1["tab_id"] as! String
        let r2 = try extractResult(sendToolCall("browser_create_tab", args: [:]))
        let id2 = r2["tab_id"] as! String

        // Close the first one
        _ = try sendToolCall("browser_close_tab", args: ["tab_id": id1])

        // List: only id2 and the default remain
        let listResp = try sendToolCall("browser_list_tabs", args: [:])
        let listResult = try extractResult(listResp)
        let tabs = listResult["tabs"] as? [[String: Any]]
        let remainingIds = tabs?.compactMap { $0["tab_id"] as? String }
        XCTAssertFalse(remainingIds?.contains(id1) ?? true, "closed tab should not appear in list")
        XCTAssertTrue(remainingIds?.contains(id2) ?? false)
    }

    func testCannotCloseLastTab() throws {
        // There's only 1 tab initially. Trying to close it should still leave 1.
        let listBefore = try extractResult(sendToolCall("browser_list_tabs", args: [:]))
        let tabsBefore = listBefore["tabs"] as? [[String: Any]]
        let firstTabId = tabsBefore?.first?["tab_id"] as! String

        let closeResp = try sendToolCall("browser_close_tab", args: ["tab_id": firstTabId])
        let closeResult = try extractResult(closeResp)
        XCTAssertEqual(closeResult["status"] as? String, "ok")

        // Verify still 1 tab
        let listAfter = try extractResult(sendToolCall("browser_list_tabs", args: [:]))
        let tabsAfter = listAfter["tabs"] as? [[String: Any]]
        XCTAssertEqual(tabsAfter?.count, 1, "closing the last tab should keep at least one")
    }

    func testActivateTab() throws {
        // Create a new tab
        let createResp = try sendToolCall("browser_create_tab", args: [:])
        let createResult = try extractResult(createResp)
        let newTabId = createResult["tab_id"] as! String

        // Default tab is active, activate the new one
        let activateResp = try sendToolCall("browser_activate_tab", args: ["tab_id": newTabId])
        let activateResult = try extractResult(activateResp)
        XCTAssertEqual(activateResult["status"] as? String, "ok")

        // List: new tab should be active
        let listResp = try sendToolCall("browser_list_tabs", args: [:])
        let listResult = try extractResult(listResp)
        let tabs = listResult["tabs"] as? [[String: Any]]
        let activeTabs = tabs?.filter { $0["is_active"] as? Bool == true }
        XCTAssertEqual(activeTabs?.count, 1)
        XCTAssertEqual(activeTabs?.first?["tab_id"] as? String, newTabId)
    }

    func testNavigateToURLAndScreenshot() throws {
        // Navigate in the default tab
        let navResp = try sendToolCall("browser_navigate", args: ["url": "https://www.bing.com"], timeout: 35)
        let navResult = try extractResult(navResp)
        XCTAssertEqual(navResult["status"] as? String, "ok")

        // Screenshot
        let ssDir = testDir.appendingPathComponent("screenshots").path
        let ssResp = try sendToolCall("browser_screenshot", args: ["output_dir": ssDir], timeout: 15)
        let ssResult = try extractResult(ssResp)
        let path = ssResult["path"] as? String
        XCTAssertNotNil(path)
        XCTAssertTrue(FileManager.default.fileExists(atPath: path!), "screenshot file should exist")
        let attrs = try FileManager.default.attributesOfItem(atPath: path!)
        let size = attrs[.size] as? Int ?? 0
        XCTAssertGreaterThan(size, 1000, "screenshot should be larger than 1KB")
    }

    func testNavigateInSpecificTab() throws {
        // Create a new tab
        let createResp = try sendToolCall("browser_create_tab", args: [:])
        let createResult = try extractResult(createResp)
        let tabId = createResult["tab_id"] as! String

        // Navigate in the new tab by tab_id
        let navResp = try sendToolCall("browser_navigate", args: ["url": "https://www.bing.com", "tab_id": tabId], timeout: 35)
        let navResult = try extractResult(navResp)
        XCTAssertEqual(navResult["status"] as? String, "ok")

        // Activate and screenshot the new tab to verify
        _ = try sendToolCall("browser_activate_tab", args: ["tab_id": tabId])
        let ssDir = testDir.appendingPathComponent("screenshots").path
        let ssResp = try sendToolCall("browser_screenshot", args: ["output_dir": ssDir, "tab_id": tabId], timeout: 15)
        let ssResult = try extractResult(ssResp)
        XCTAssertNotNil(ssResult["path"] as? String)
    }

    func testNavigateInMultipleTabs() throws {
        // Create two tabs
        let r1 = try extractResult(sendToolCall("browser_create_tab", args: [:]))
        let tabA = r1["tab_id"] as! String
        let r2 = try extractResult(sendToolCall("browser_create_tab", args: [:]))
        let tabB = r2["tab_id"] as! String

        // Navigate tab A
        let navAResp = try sendToolCall("browser_navigate", args: ["url": "https://www.bing.com", "tab_id": tabA], timeout: 35)
        XCTAssertNotNil(try? extractResult(navAResp))

        // Navigate tab B to a different site
        let navBResp = try sendToolCall("browser_navigate", args: ["url": "https://www.google.com", "tab_id": tabB], timeout: 35)
        XCTAssertNotNil(try? extractResult(navBResp))

        // Evaluate JS in tab A to verify it's on bing
        let evalResp = try sendToolCall("browser_evaluate", args: ["expression": "window.location.hostname", "tab_id": tabA])
        let evalResult = try extractResult(evalResp)
        let hostname = evalResult["result"] as? String
        XCTAssertEqual(hostname, "www.bing.com")
    }

    // MARK: - Console capture tests

    func testConsoleLogCapture() throws {
        // Load a data URL with inline JS that produces console output
        let html = "<html><body><script>" +
            "console.log('test log message');" +
            "console.warn('test warn message');" +
            "console.error('test error message');" +
            "</script></body></html>"
        let encoded = html.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!
        let dataURL = "data:text/html," + encoded

        let navResp = try sendToolCall("browser_navigate", args: ["url": dataURL], timeout: 15)
        let navResult = try extractResult(navResp)
        XCTAssertEqual(navResult["status"] as? String, "ok")

        // Give the page a moment to execute scripts
        Thread.sleep(forTimeInterval: 0.5)

        // Retrieve logs
        let logsResp = try sendToolCall("browser_get_logs", args: [:])
        let logsResult = try extractResult(logsResp)
        let logs = logsResult["logs"] as? [[String: Any]]
        XCTAssertNotNil(logs, "should return a logs array")
        XCTAssertGreaterThanOrEqual(logs?.count ?? 0, 3, "should capture at least 3 console calls")

        // Verify specific log entries exist
        let messages = logs?.compactMap { $0["message"] as? String } ?? []
        let types = logs?.compactMap { $0["type"] as? String } ?? []

        XCTAssertTrue(messages.contains(where: { $0.contains("test log message") }), "should contain log message")
        XCTAssertTrue(messages.contains(where: { $0.contains("test warn message") }), "should contain warn message")
        XCTAssertTrue(messages.contains(where: { $0.contains("test error message") }), "should contain error message")
        XCTAssertTrue(types.contains("log"), "should have log type")
        XCTAssertTrue(types.contains("warn"), "should have warn type")
        XCTAssertTrue(types.contains("error"), "should have error type")
    }

    func testConsoleGetLogsClearsBuffer() throws {
        let html = "<html><body><script>console.log('first call');</script></body></html>"
        let encoded = html.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!
        let dataURL = "data:text/html," + encoded

        let navResp = try sendToolCall("browser_navigate", args: ["url": dataURL], timeout: 15)
        XCTAssertNotNil(try? extractResult(navResp))
        Thread.sleep(forTimeInterval: 0.5)

        // First read — should have the log
        let r1 = try extractResult(sendToolCall("browser_get_logs", args: [:]))
        let logs1 = r1["logs"] as? [[String: Any]] ?? []
        XCTAssertGreaterThanOrEqual(logs1.count, 1)

        // Second read — buffer should be cleared
        let r2 = try extractResult(sendToolCall("browser_get_logs", args: [:]))
        let logs2 = r2["logs"] as? [[String: Any]] ?? []
        XCTAssertEqual(logs2.count, 0, "buffer should be empty after first read")
    }

    func testConsoleLogTimestamps() throws {
        let html = "<html><body><script>console.log('timed log');</script></body></html>"
        let encoded = html.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!
        let dataURL = "data:text/html," + encoded

        let navResp = try sendToolCall("browser_navigate", args: ["url": dataURL], timeout: 15)
        XCTAssertNotNil(try? extractResult(navResp))
        Thread.sleep(forTimeInterval: 0.5)

        let r = try extractResult(sendToolCall("browser_get_logs", args: [:]))
        let logs = r["logs"] as? [[String: Any]] ?? []
        XCTAssertGreaterThanOrEqual(logs.count, 1)

        // Verify all entries have ISO8601 timestamps
        for log in logs {
            let ts = log["timestamp"] as? String
            XCTAssertNotNil(ts, "each log should have a timestamp")
            XCTAssertFalse(ts!.isEmpty, "timestamp should not be empty")
        }
    }

    func testConsoleGetLogsFilterByType() throws {
        let html = "<html><body><script>" +
            "console.log('just a log');" +
            "console.warn('just a warn');" +
            "console.error('just an error');" +
            "</script></body></html>"
        let encoded = html.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!
        let dataURL = "data:text/html," + encoded

        let navResp = try sendToolCall("browser_navigate", args: ["url": dataURL], timeout: 15)
        XCTAssertNotNil(try? extractResult(navResp))
        Thread.sleep(forTimeInterval: 0.5)

        // Filter by type=error
        let resp = try sendToolCall("browser_get_logs", args: ["type": "error"])
        let result = try extractResult(resp)
        let logs = result["logs"] as? [[String: Any]] ?? []
        XCTAssertEqual(logs.count, 1, "should return exactly one error log")
        XCTAssertEqual(logs.first?["type"] as? String, "error")
        XCTAssertTrue((logs.first?["message"] as? String)?.contains("just an error") ?? false)
    }

    func testConsoleGetLogsSearchByMessage() throws {
        let html = "<html><body><script>" +
            "console.log('connection timeout to server');" +
            "console.warn('disk space low');" +
            "console.error('request timed out');" +
            "</script></body></html>"
        let encoded = html.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!
        let dataURL = "data:text/html," + encoded

        let navResp = try sendToolCall("browser_navigate", args: ["url": dataURL], timeout: 15)
        XCTAssertNotNil(try? extractResult(navResp))
        Thread.sleep(forTimeInterval: 0.5)

        // Search for "timeout" — should match 2 entries (log + error)
        let resp = try sendToolCall("browser_get_logs", args: ["search": "timeout"])
        let result = try extractResult(resp)
        let logs = result["logs"] as? [[String: Any]] ?? []
        XCTAssertEqual(logs.count, 2, "should find 2 logs containing 'timeout'")
        for log in logs {
            let msg = log["message"] as? String ?? ""
            XCTAssertTrue(msg.localizedCaseInsensitiveContains("timeout"))
        }
    }

    func testConsoleGetLogsFilterAndSearch() throws {
        let html = "<html><body><script>" +
            "console.log('user logged in');" +
            "console.warn('login attempt failed');" +
            "console.error('login error: db timeout');" +
            "console.warn('cache miss');" +
            "</script></body></html>"
        let encoded = html.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!
        let dataURL = "data:text/html," + encoded

        let navResp = try sendToolCall("browser_navigate", args: ["url": dataURL], timeout: 15)
        XCTAssertNotNil(try? extractResult(navResp))
        Thread.sleep(forTimeInterval: 0.5)

        // Filter by type=warn + search="login" — should match only the warn entry
        let resp = try sendToolCall("browser_get_logs", args: ["type": "warn", "search": "login"])
        let result = try extractResult(resp)
        let logs = result["logs"] as? [[String: Any]] ?? []
        XCTAssertEqual(logs.count, 1, "should find 1 warn log containing 'login'")
        XCTAssertEqual(logs.first?["type"] as? String, "warn")
        XCTAssertTrue((logs.first?["message"] as? String)?.localizedCaseInsensitiveContains("login") ?? false)
    }

    func testConsoleGetLogsFilterUnknownType() throws {
        // With no logs matching the filter, should return empty array (not nil)
        let resp = try sendToolCall("browser_get_logs", args: ["type": "debug"])
        let result = try extractResult(resp)
        let logs = result["logs"] as? [[String: Any]] ?? []
        XCTAssertEqual(logs.count, 0, "should return empty array when no logs match")
    }

    // MARK: - Error handling

    func testNavigateWithInvalidURL() throws {
        let resp = try sendToolCall("browser_navigate", args: ["url": ""])
        // Should get an error response
        let body = try JSONSerialization.jsonObject(with: resp) as? [String: Any]
        XCTAssertNotNil(body?["error"], "empty URL should cause validation error")
    }

    func testCloseTabWithMissingTabId() throws {
        let resp = try sendToolCall("browser_close_tab", args: [:])
        let body = try JSONSerialization.jsonObject(with: resp) as? [String: Any]
        XCTAssertNotNil(body?["error"], "missing tab_id should cause validation error")
    }

    func testActivateTabWithInvalidTabId() throws {
        let resp = try sendToolCall("browser_activate_tab", args: ["tab_id": "nonexistent-tab"])
        // Activate with wrong ID is a no-op — should succeed but not change active tab
        let result = try extractResult(resp)
        XCTAssertEqual(result["status"] as? String, "ok")
    }

    func testNavigateInNonexistentTab() throws {
        // Creating the tab should succeed but navigation should fail silently
        // (falls back to active tab)
        let resp = try sendToolCall("browser_navigate", args: ["url": "about:blank", "tab_id": "does-not-exist"], timeout: 5)
        let result = try extractResult(resp)
        XCTAssertEqual(result["status"] as? String, "ok")
    }

    // MARK: - Helpers

    /// Send a tools/call and return the raw response data.
    @discardableResult
    private func sendToolCall(_ name: String, args: [String: String], timeout: TimeInterval = 10) throws -> Data {
        let messageURL = baseURL.appendingPathComponent("message")
        var request = URLRequest(url: messageURL)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.timeoutInterval = timeout

        let body: [String: Any] = [
            "jsonrpc": "2.0",
            "id": Int.random(in: 1...9999),
            "method": "tools/call",
            "params": [
                "name": name,
                "arguments": args,
            ] as [String: Any],
        ]
        request.httpBody = try JSONSerialization.data(withJSONObject: body)

        let semaphore = DispatchSemaphore(value: 0)
        let box = Box<Data?>(nil)

        let task = URLSession.shared.dataTask(with: request) { data, _, _ in
            box.value = data
            semaphore.signal()
        }
        task.resume()
        semaphore.wait()

        return box.value ?? Data()
    }

    /// Extract the `result` dictionary from a tools/call response.
    private func extractResult(_ data: Data) throws -> [String: Any] {
        guard let body = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw TestError("response is not a JSON object")
        }
        if let error = body["error"] as? [String: Any] {
            throw TestError("RPC error: \(error)")
        }
        guard let result = body["result"] as? [String: Any],
              let content = result["content"] as? [[String: Any]],
              let text = content.first?["text"] as? String,
              let resultData = text.data(using: .utf8),
              let resultObj = try? JSONSerialization.jsonObject(with: resultData) as? [String: Any] else {
            throw TestError("could not extract result from response: \(body)")
        }
        return resultObj
    }

    private func waitForServer(timeout: TimeInterval) throws {
        let deadline = Date().addingTimeInterval(timeout)
        let testURL = baseURL.appendingPathComponent("message")

        while Date() < deadline {
            var request = URLRequest(url: testURL)
            request.httpMethod = "POST"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: [
                "jsonrpc": "2.0",
                "id": 1,
                "method": "tools/list",
            ])

            let semaphore = DispatchSemaphore(value: 0)
            let okBox = Box(false)
            let task = URLSession.shared.dataTask(with: request) { _, resp, _ in
                if let httpResp = resp as? HTTPURLResponse, httpResp.statusCode == 200 {
                    okBox.value = true
                }
                semaphore.signal()
            }
            task.resume()
            semaphore.wait()

            if okBox.value { return }
            Thread.sleep(forTimeInterval: 0.2)
        }
        throw TestError("server did not start within \(timeout)s")
    }

    private func locateBinary() -> URL? {
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

/// Thread-safe container for capturing values in @Sendable closures.
final class Box<T: Sendable>: @unchecked Sendable {
    var value: T
    init(_ value: T) { self.value = value }
}

struct TestError: Error, CustomStringConvertible {
    let message: String
    init(_ message: String) { self.message = message }
    var description: String { message }
}
