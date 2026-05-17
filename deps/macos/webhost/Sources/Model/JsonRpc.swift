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

struct JsonRpcRequest: Codable, Sendable {
    let jsonrpc: String
    let id: Any?
    let method: String
    let params: JsonRpcParams?

    init(jsonrpc: String = "2.0", id: Any? = nil, method: String, params: JsonRpcParams? = nil) {
        self.jsonrpc = jsonrpc
        self.id = id
        self.method = method
        self.params = params
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

struct JsonRpcResponse: Codable, Sendable {
    let jsonrpc: String
    let id: Any?
    let result: JsonRpcResult?
    let error: JsonRpcErrorResponse?

    init(id: Any?, result: JsonRpcResult) {
        self.jsonrpc = "2.0"
        self.id = id
        self.result = result
        self.error = nil
    }

    init(id: Any?, error: JsonRpcError) {
        self.jsonrpc = "2.0"
        self.id = id
        self.result = nil
        self.error = JsonRpcErrorResponse(code: error.code, message: error.message)
    }
}

struct JsonRpcResult: Codable, Sendable {
    let success: Bool
    let sessionId: String?
    let value: String?
    let data: String?
    let url: String?
    let title: String?
    let status: String?
    let capabilities: [String: Bool]?

    init(success: Bool = true, sessionId: String? = nil, value: String? = nil,
         data: String? = nil, url: String? = nil, title: String? = nil,
         status: String? = nil, capabilities: [String: Bool]? = nil) {
        self.success = success
        self.sessionId = sessionId
        self.value = value
        self.data = data
        self.url = url
        self.title = title
        self.status = status
        self.capabilities = capabilities
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