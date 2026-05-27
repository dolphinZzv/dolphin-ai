import Foundation

enum JsonRpcError: Error, Sendable {
    case parseError
    case methodNotFound
    case invalidParams
    case internalError
    case sessionNotFound
    case sessionLimitExceeded
    case navigationTimeout
    case scriptTimeout
}

extension JsonRpcError {
    var code: Int {
        switch self {
        case .parseError: return -32700
        case .methodNotFound: return -32601
        case .invalidParams: return -32602
        case .internalError: return -32603
        case .sessionNotFound: return -32000
        case .sessionLimitExceeded: return -32001
        case .navigationTimeout: return -32002
        case .scriptTimeout: return -32003
        }
    }

    var message: String {
        switch self {
        case .parseError: return "Parse error"
        case .methodNotFound: return "Method not found"
        case .invalidParams: return "Invalid params"
        case .internalError: return "Internal error"
        case .sessionNotFound: return "Session not found"
        case .sessionLimitExceeded: return "Session limit exceeded"
        case .navigationTimeout: return "Navigation timeout"
        case .scriptTimeout: return "Script timeout"
        }
    }
}

struct JsonRpcRequest: Sendable {
    let id: Any?
    let method: String
    let params: JsonRpcParams?

    init(id: Any?, method: String, params: [String: Any]? = nil) {
        self.id = id
        self.method = method
        if let p = params {
            // Name can be String directly or wrapped in AnyCodable (from test JSON construction)
            let name: String
            if let s = p["name"] as? String { name = s }
            else if let ac = p["name"] as? AnyCodable, let s = ac.value as? String { name = s }
            else { name = method }

            // Arguments can be [String: Any] directly or wrapped in AnyCodable
            let args: [String: Any]
            if let dict = p["arguments"] as? [String: Any] { args = dict }
            else if let ac = p["arguments"] as? AnyCodable, let dict = ac.value as? [String: Any] { args = dict }
            else { args = p }

            self.params = JsonRpcParams(name: name, arguments: args.mapValues { v in
                (v as? AnyCodable) ?? AnyCodable(v)
            })
        } else {
            self.params = nil
        }
    }
}

struct JsonRpcParams: Codable, Sendable {
    let name: String?
    let arguments: [String: AnyCodable]?

    init(name: String, arguments: [String: AnyCodable]? = nil) {
        self.name = name
        self.arguments = arguments
    }

    func stringArg(_ key: String) -> String? {
        arguments?[key]?.value as? String
    }

    func intArg(_ key: String) -> Int? {
        arguments?[key]?.value as? Int
    }

    func boolArg(_ key: String) -> Bool? {
        arguments?[key]?.value as? Bool
    }
}

struct JsonRpcResponse: Sendable {
    let id: Any?
    let result: JsonRpcResult?
    let error: JsonRpcErrorResponse?

    init(id: Any?, result: JsonRpcResult) {
        self.id = id
        self.result = result
        self.error = nil
    }

    init(id: Any?, error: JsonRpcError) {
        self.id = id
        self.result = nil
        self.error = JsonRpcErrorResponse(code: error.code, message: error.message)
    }

    func toJson() -> String {
        var dict: [String: Any] = ["jsonrpc": "2.0"]
        if let id = id {
            dict["id"] = id
        }
        if let result = result {
            dict["result"] = result.toDictionary()
        }
        if let error = error {
            dict["error"] = ["code": error.code, "message": error.message]
        }
        guard let data = try? JSONSerialization.data(withJSONObject: dict),
              let str = String(data: data, encoding: .utf8) else {
            return "{\"jsonrpc\":\"2.0\",\"id\":null,\"error\":{\"code\":-32603,\"message\":\"encode failed\"}}"
        }
        return str
    }
}

struct JsonRpcResult: Sendable {
    let success: Bool
    let sessionId: String?
    let value: String?
    let data: String?
    let url: String?
    let title: String?
    let status: String?
    let capabilities: [String: AnyCodable]?
    let found: Bool?
    let extra: [String: Any]?

    init(success: Bool = true, sessionId: String? = nil, value: String? = nil,
         data: String? = nil, url: String? = nil, title: String? = nil,
         status: String? = nil, capabilities: [String: AnyCodable]? = nil,
         found: Bool? = nil, extra: [String: Any]? = nil) {
        self.success = success
        self.sessionId = sessionId
        self.value = value
        self.data = data
        self.url = url
        self.title = title
        self.status = status
        self.capabilities = capabilities
        self.found = found
        self.extra = extra
    }

    func toDictionary() -> [String: Any] {
        var d: [String: Any] = ["success": success]
        if let v = sessionId { d["sessionId"] = v }
        if let v = value { d["value"] = v }
        if let v = data { d["data"] = v }
        if let v = url { d["url"] = v }
        if let v = title { d["title"] = v }
        if let v = status { d["status"] = v }
        if let v = capabilities { d["capabilities"] = v.mapValues { $0.value } }
        if let v = found { d["found"] = v }
        if let v = extra { d.merge(v) { (_, new) in new } }
        return d
    }
}

struct JsonRpcErrorResponse: Codable, Sendable {
    let code: Int
    let message: String
}

struct AnyCodable: Codable, Sendable {
    let value: Any

    init(_ value: Any) {
        self.value = value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let intValue = try? container.decode(Int.self) {
            value = intValue
        } else if let doubleValue = try? container.decode(Double.self) {
            value = doubleValue
        } else if let stringValue = try? container.decode(String.self) {
            value = stringValue
        } else if let boolValue = try? container.decode(Bool.self) {
            value = boolValue
        } else if let arrayValue = try? container.decode([AnyCodable].self) {
            value = arrayValue.map { $0.value }
        } else if let dictValue = try? container.decode([String: AnyCodable].self) {
            value = dictValue.mapValues { $0.value }
        } else {
            value = NSNull()
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch value {
        case let intValue as Int:
            try container.encode(intValue)
        case let doubleValue as Double:
            try container.encode(doubleValue)
        case let stringValue as String:
            try container.encode(stringValue)
        case let boolValue as Bool:
            try container.encode(boolValue)
        default:
            try container.encodeNil()
        }
    }
}