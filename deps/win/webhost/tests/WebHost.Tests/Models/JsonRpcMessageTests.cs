using System.Text.Json;
using Dolphin.WebHost.Models;
using Xunit;
using Xunit.Abstractions;

namespace Dolphin.WebHost.Tests.Models
{
    public class JsonRpcMessageTests
    {
        private static readonly JsonSerializerOptions JsonOpts = new()
        {
            PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        };

        [Fact]
        public void Deserialize_Request_With_String_Id()
        {
            var json = "{\"jsonrpc\":\"2.0\",\"id\":\"1\",\"method\":\"tools/call\",\"params\":{\"name\":\"test\"}}";
            var req = JsonSerializer.Deserialize<JsonRpcRequest>(json, JsonOpts);

            Assert.NotNull(req);
            Assert.Equal("2.0", req!.JsonRpc);
            Assert.Equal("tools/call", req.Method);
            Assert.True(req.Id.HasValue);
            Assert.Equal("1", req.Id.Value.GetString());
        }

        [Fact]
        public void Deserialize_Request_With_Numeric_Id()
        {
            var json = "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{}}";
            var req = JsonSerializer.Deserialize<JsonRpcRequest>(json, JsonOpts);

            Assert.NotNull(req);
            Assert.Equal(1, req!.Id.Value.GetInt32());
        }

        [Fact]
        public void Deserialize_Request_Without_Id_IsNotification()
        {
            var json = "{\"jsonrpc\":\"2.0\",\"method\":\"web/ping\",\"params\":{}}";
            var req = JsonSerializer.Deserialize<JsonRpcRequest>(json, JsonOpts);

            Assert.NotNull(req);
            Assert.False(req!.Id.HasValue);
        }

        [Fact]
        public void Deserialize_Request_With_Arguments()
        {
            var json = "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"page_open\",\"arguments\":{\"sessionId\":\"sess_abc\",\"url\":\"https://example.com\"}}}";
            var req = JsonSerializer.Deserialize<JsonRpcRequest>(json, JsonOpts);

            Assert.NotNull(req);
            Assert.NotNull(req!.Params);

            var rpcParams = JsonSerializer.Deserialize<ToolsCallParams>(req.Params.Value.GetRawText(), JsonOpts);
            Assert.NotNull(rpcParams);
            Assert.Equal("page_open", rpcParams!.Name);
            Assert.True(rpcParams.Arguments.HasValue);

            var args = rpcParams.Arguments.Value;
            Assert.Equal("sess_abc", args.GetProperty("sessionId").GetString());
            Assert.Equal("https://example.com", args.GetProperty("url").GetString());
        }

        [Fact]
        public void Deserialize_Request_Missing_Method_Defaults_To_Empty()
        {
            var json = "{\"jsonrpc\":\"2.0\",\"id\":1}";
            var req = JsonSerializer.Deserialize<JsonRpcRequest>(json, JsonOpts);

            Assert.NotNull(req);
            Assert.Equal("", req!.Method);
        }

        [Fact]
        public void Serialize_Error_Response()
        {
            var response = new JsonRpcResponse
            {
                JsonRpc = "2.0",
                Id = JsonDocument.Parse("1").RootElement,
                Result = null,
                Error = new JsonRpcError { Code = -32602, Message = "Invalid params" },
            };

            var json = JsonSerializer.Serialize(response, JsonOpts);
            var doc = JsonDocument.Parse(json);

            Assert.Equal("2.0", doc.RootElement.GetProperty("jsonrpc").GetString());
            Assert.Equal(-32602, doc.RootElement.GetProperty("error").GetProperty("code").GetInt32());
        }

        [Fact]
        public void ErrorCode_Constants()
        {
            Assert.Equal(-32600, JsonRpcErrorCodes.InvalidRequest);
            Assert.Equal(-32602, JsonRpcErrorCodes.InvalidParams);
            Assert.Equal(-32000, JsonRpcErrorCodes.SessionNotFound);
            Assert.Equal(-32603, JsonRpcErrorCodes.InternalError);
        }
    }
}