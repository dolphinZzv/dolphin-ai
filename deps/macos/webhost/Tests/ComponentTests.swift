import XCTest
@testable import WebHost

final class BlockerViewTests: XCTestCase {
    func testBlockerViewDefaults() {
        let blocker = BlockerView(frame: NSRect(x: 0, y: 0, width: 100, height: 100))

        XCTAssertFalse(blocker.acceptsFirstResponder)
    }
}

final class AnyCodableTests: XCTestCase {
    func testStringEncoding() throws {
        let codable = AnyCodable("test")
        let data = try JSONEncoder().encode(codable)
        let json = String(data: data, encoding: .utf8)!
        XCTAssertTrue(json.contains("test"))
    }

    func testIntEncoding() throws {
        let codable = AnyCodable(42)
        let data = try JSONEncoder().encode(codable)
        let json = String(data: data, encoding: .utf8)!
        XCTAssertTrue(json.contains("42"))
    }

    func testBoolEncoding() throws {
        let codable = AnyCodable(true)
        let data = try JSONEncoder().encode(codable)
        let json = String(data: data, encoding: .utf8)!
        XCTAssertTrue(json.contains("true"))
    }

    func testDoubleEncoding() throws {
        let codable = AnyCodable(3.14)
        let data = try JSONEncoder().encode(codable)
        let json = String(data: data, encoding: .utf8)!
        XCTAssertTrue(json.contains("3.14"))
    }

    func testNullEncoding() throws {
        let codable = AnyCodable(NSNull())
        let data = try JSONEncoder().encode(codable)
        let json = String(data: data, encoding: .utf8)!
        XCTAssertTrue(json.contains("null"))
    }

    func testDecodingFromString() throws {
        let json = "\"test string\"".data(using: .utf8)!
        let codable = try JSONDecoder().decode(AnyCodable.self, from: json)
        XCTAssertEqual(codable.value as? String, "test string")
    }

    func testDecodingFromInt() throws {
        let json = "123".data(using: .utf8)!
        let codable = try JSONDecoder().decode(AnyCodable.self, from: json)
        XCTAssertEqual(codable.value as? Int, 123)
    }

    func testDecodingFromBool() throws {
        let json = "true".data(using: .utf8)!
        let codable = try JSONDecoder().decode(AnyCodable.self, from: json)
        XCTAssertEqual(codable.value as? Bool, true)
    }

    func testDecodingFromDouble() throws {
        let json = "3.14".data(using: .utf8)!
        let codable = try JSONDecoder().decode(AnyCodable.self, from: json)
        XCTAssertEqual(codable.value as? Double, 3.14)
    }
}