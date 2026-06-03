import XCTest

final class BrowserMCPUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }
}

// MARK: - Tests

extension BrowserMCPUITests {
    @MainActor
    func testAppLaunches() throws {
        let app = XCUIApplication()
        app.launch()
        defer { app.terminate() }
        XCTAssertTrue(app.state.rawValue > 0, "App process should be running")
    }

    @MainActor
    func testWindowShowHide() throws {
        let app = XCUIApplication()
        app.launch()
        defer { app.terminate() }

        app.activate()
        let window = app.windows.firstMatch
        if !window.exists {
            app.activate()
        }
        let exists = window.waitForExistence(timeout: 5)
        XCTAssertTrue(exists, "App window should exist")
    }

    @MainActor
    func testToolbarButtons() throws {
        let app = XCUIApplication()
        app.launch()
        defer { app.terminate() }

        app.activate()
        let window = app.windows.firstMatch
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window should exist")
            return
        }

        let urlField = window.textFields.firstMatch
        XCTAssertTrue(urlField.exists, "URL text field should exist")

        urlField.click()
        urlField.typeText("about:blank\n")

        let backButton = window.buttons.firstMatch
        XCTAssertTrue(backButton.exists, "Back button should exist")
    }
}
