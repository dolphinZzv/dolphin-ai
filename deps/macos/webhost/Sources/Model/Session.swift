import Foundation

struct Session: Identifiable, Sendable {
    let id: String
    let browser: String
    let platform: String
    var state: String
    let pid: pid_t
    let viewport: Viewport
    let createdAt: Date
    var lastUsed: Date

    init(id: String = UUID().uuidString,
         browser: String = "webkit",
         platform: String = "darwin",
         state: String = "active",
         pid: pid_t = ProcessInfo.processInfo.processIdentifier,
         viewport: Viewport,
         createdAt: Date = Date(),
         lastUsed: Date = Date()) {
        self.id = id
        self.browser = browser
        self.platform = platform
        self.state = state
        self.pid = pid
        self.viewport = viewport
        self.createdAt = createdAt
        self.lastUsed = lastUsed
    }
}

struct Viewport: Codable, Sendable {
    var width: Int
    var height: Int

    init(width: Int = 1920, height: Int = 1080) {
        self.width = width
        self.height = height
    }
}

struct SessionMeta: Codable {
    let version: Int
    let id: String
    let browser: String
    let platform: String
    let state: String
    let pid: pid_t
    let viewport: Viewport
    let createdAt: String
    let lastUsed: String
}