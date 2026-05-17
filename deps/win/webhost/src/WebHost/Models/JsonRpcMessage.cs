using System.Text.Json;
using System.Text.Json.Serialization;

namespace Dolphin.WebHost.Models
{
    public class JsonRpcRequest
    {
        [JsonPropertyName("jsonrpc")]
        public string JsonRpc { get; set; } = "2.0";

        [JsonPropertyName("id")]
        public JsonElement? Id { get; set; }

        [JsonPropertyName("method")]
        public string Method { get; set; } = string.Empty;

        [JsonPropertyName("params")]
        public JsonElement? Params { get; set; }
    }

    public class JsonRpcResponse
    {
        [JsonPropertyName("jsonrpc")]
        public string JsonRpc { get; set; } = "2.0";

        [JsonPropertyName("id")]
        public JsonElement? Id { get; set; }

        [JsonPropertyName("result")]
        public object? Result { get; set; }

        [JsonPropertyName("error")]
        public JsonRpcError? Error { get; set; }
    }

    public class JsonRpcError
    {
        [JsonPropertyName("code")]
        public int Code { get; set; }

        [JsonPropertyName("message")]
        public string Message { get; set; } = string.Empty;

        [JsonPropertyName("data")]
        public object? Data { get; set; }
    }

    public class JsonRpcNotification
    {
        [JsonPropertyName("jsonrpc")]
        public string JsonRpc { get; set; } = "2.0";

        [JsonPropertyName("method")]
        public string Method { get; set; } = string.Empty;

        [JsonPropertyName("params")]
        public object? Params { get; set; }
    }

    public static class JsonRpcErrorCodes
    {
        public const int InvalidRequest = -32600;
        public const int InvalidParams = -32602;
        public const int SessionNotFound = -32000;
        public const int SessionLimitExceeded = -32001;
        public const int NavigationTimeout = -32002;
        public const int ScriptTimeout = -32003;
        public const int InternalError = -32603;
    }

    public class ToolsCallParams
    {
        [JsonPropertyName("name")]
        public string Name { get; set; } = string.Empty;

        [JsonPropertyName("arguments")]
        public JsonElement? Arguments { get; set; }
    }
}
