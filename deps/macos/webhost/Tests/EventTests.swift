import XCTest
@testable import WebHost

final class EventTests: XCTestCase {
    func testEventCreation() {
        let event = Event(method: "web/console", params: ["message": "test"])

        XCTAssertEqual(event.method, "web/console")
        XCTAssertTrue(event.t > 0)
        XCTAssertEqual(event.params["message"] as? String, "test")
    }

    func testEventToJson() {
        let event = Event(method: "web/console", params: ["message": "hello", "level": "log"])
        let json = event.toJson()

        XCTAssertTrue(json.contains("web/console"))
        XCTAssertTrue(json.contains("hello"))
        XCTAssertTrue(json.contains("log"))
        XCTAssertTrue(json.hasSuffix("\n"))
    }
}

final class WebEventTests: XCTestCase {
    func testConsoleEvent() {
        let event = WebEvent.console("test message", level: "info")

        XCTAssertEqual(event.method, "web/console")
        XCTAssertEqual(event.params["message"] as? String, "test message")
        XCTAssertEqual(event.params["level"] as? String, "info")
    }

    func testNavigationEvent() {
        let event = WebEvent.navigation("https://example.com", status: "complete", progress: 100)

        XCTAssertEqual(event.method, "web/navigation")
        XCTAssertEqual(event.params["url"] as? String, "https://example.com")
        XCTAssertEqual(event.params["status"] as? String, "complete")
        XCTAssertEqual(event.params["progress"] as? Int, 100)
    }

    func testNavigationEventWithoutProgress() {
        let event = WebEvent.navigation("https://example.com", status: "loading")

        XCTAssertEqual(event.method, "web/navigation")
        XCTAssertEqual(event.params["status"] as? String, "loading")
        XCTAssertNil(event.params["progress"])
    }

    func testErrorEvent() {
        let event = WebEvent.error("Uncaught error", stack: "at line 42")

        XCTAssertEqual(event.method, "web/error")
        XCTAssertEqual(event.params["message"] as? String, "Uncaught error")
        XCTAssertEqual(event.params["stack"] as? String, "at line 42")
    }

    func testErrorEventWithoutStack() {
        let event = WebEvent.error("Simple error")

        XCTAssertEqual(event.method, "web/error")
        XCTAssertNil(event.params["stack"])
    }

    func testDialogEvent() {
        let event = WebEvent.dialog("confirm", message: "Delete?", dialogId: "dlg_001")

        XCTAssertEqual(event.method, "web/dialog")
        XCTAssertEqual(event.params["type"] as? String, "confirm")
        XCTAssertEqual(event.params["message"] as? String, "Delete?")
        XCTAssertEqual(event.params["dialogId"] as? String, "dlg_001")
    }

    func testPopupEvent() {
        let event = WebEvent.popup(url: "https://popup.example.com", popupId: "pop_001")

        XCTAssertEqual(event.method, "web/popup")
        XCTAssertEqual(event.params["url"] as? String, "https://popup.example.com")
        XCTAssertEqual(event.params["popupId"] as? String, "pop_001")
    }

    func testPingEvent() {
        let event = WebEvent.ping()

        XCTAssertEqual(event.method, "web/ping")
        XCTAssertTrue(event.t > 0)
    }
}

final class EventBufferTests: XCTestCase {
    func testAppendAndGetEvents() {
        let buffer = EventBuffer()
        let event1 = Event(method: "web/console", params: ["msg": "1"])
        let event2 = Event(method: "web/console", params: ["msg": "2"])

        buffer.append(event1)
        buffer.append(event2)

        let events = buffer.getEvents(since: 0)
        XCTAssertEqual(events.count, 2)
    }

    func testGetEventsWithSince() {
        let buffer = EventBuffer()
        let now = Int64(Date().timeIntervalSince1970)

        let event1 = Event(method: "web/console", params: ["msg": "1"])
        let event2 = Event(method: "web/console", params: ["msg": "2"])

        buffer.append(event1)
        buffer.append(event2)

        let events = buffer.getEvents(since: event1.t)
        XCTAssertEqual(events.count, 1)
        XCTAssertEqual(events[0].params["msg"] as? String, "2")
    }

    func testBufferMaxSize() {
        let buffer = EventBuffer()
        let baseTime = Int64(Date().timeIntervalSince1970)

        for i in 0..<1500 {
            let event = Event(method: "web/console", params: ["msg": "\(i)"])
            buffer.append(event)
        }

        let events = buffer.getEvents(since: baseTime)
        XCTAssertLessThanOrEqual(events.count, 1000)
    }
}