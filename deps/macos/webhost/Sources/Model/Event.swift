import Foundation

struct Event: Sendable {
    let t: Int64
    let method: String
    let params: [String: Any]

    init(method: String, params: [String: Any]) {
        self.t = Int64(Date().timeIntervalSince1970)
        self.method = method
        self.params = params
    }

    func toJson() -> String {
        let paramsJson = params.mapValues { AnyCodable($0) }
        let wrapper = EventWrapper(jsonrpc: "2.0", method: method, params: paramsJson)
        guard let data = try? JSONEncoder().encode(wrapper),
              let json = String(data: data, encoding: .utf8) else {
            return ""
        }
        return json + "\n"
    }
}

private struct EventWrapper: Codable {
    let jsonrpc: String
    let method: String
    let params: [String: AnyCodable]
}

struct WebEvent {
    static func console(_ message: String, level: String = "log") -> Event {
        Event(method: "web/console", params: ["message": message, "level": level])
    }

    static func navigation(_ url: String, status: String, progress: Int? = nil) -> Event {
        var params = ["url": url, "status": status] as [String: Any]
        if let p = progress { params["progress"] = String(p) }
        return Event(method: "web/navigation", params: params)
    }

    static func error(_ message: String, stack: String? = nil) -> Event {
        var params = ["message": message]
        if let s = stack { params["stack"] = s }
        return Event(method: "web/error", params: params)
    }

    static func dialog(_ type: String, message: String, dialogId: String) -> Event {
        Event(method: "web/dialog", params: ["type": type, "message": message, "dialogId": dialogId])
    }

    static func popup(_ url: String, popupId: String) -> Event {
        Event(method: "web/popup", params: ["url": url, "popupId": popupId])
    }

    static func ping() -> Event {
        Event(method: "web/ping", params: ["t": Int64(Date().timeIntervalSince1970)])
    }
}