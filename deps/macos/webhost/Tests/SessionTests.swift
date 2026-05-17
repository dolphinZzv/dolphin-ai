import XCTest
@testable import WebHost

final class SessionTests: XCTestCase {
    func testSessionCreation() {
        let viewport = Viewport(width: 1920, height: 1080)
        let session = Session(viewport: viewport)

        XCTAssertFalse(session.id.isEmpty)
        XCTAssertEqual(session.browser, "webkit")
        XCTAssertEqual(session.platform, "darwin")
        XCTAssertEqual(session.state, "active")
        XCTAssertEqual(session.viewport.width, 1920)
        XCTAssertEqual(session.viewport.height, 1080)
    }

    func testSessionIdIsUnique() {
        let viewport = Viewport()
        let session1 = Session(viewport: viewport)
        let session2 = Session(viewport: viewport)

        XCTAssertNotEqual(session1.id, session2.id)
    }
}

final class ViewportTests: XCTestCase {
    func testDefaultViewport() {
        let viewport = Viewport()

        XCTAssertEqual(viewport.width, 1920)
        XCTAssertEqual(viewport.height, 1080)
    }

    func testCustomViewport() {
        let viewport = Viewport(width: 1280, height: 720)

        XCTAssertEqual(viewport.width, 1280)
        XCTAssertEqual(viewport.height, 720)
    }
}

final class SessionMetaTests: XCTestCase {
    func testSessionMetaEncoding() {
        let meta = SessionMeta(
            version: 1,
            id: "test-session",
            browser: "webkit",
            platform: "darwin",
            state: "active",
            pid: 12345,
            viewport: Viewport(width: 1920, height: 1080),
            createdAt: "2026-05-17T10:30:00Z",
            lastUsed: "2026-05-17T10:35:00Z"
        )

        let encoder = JSONEncoder()
        let data = try! encoder.encode(meta)
        let json = String(data: data, encoding: .utf8)!

        XCTAssertTrue(json.contains("test-session"))
        XCTAssertTrue(json.contains("webkit"))
        XCTAssertTrue(json.contains("darwin"))
    }
}