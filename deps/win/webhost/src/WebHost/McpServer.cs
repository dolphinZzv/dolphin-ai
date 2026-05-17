using System;
using System.IO;
using System.Net;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using Dolphin.WebHost.Models;

namespace Dolphin.WebHost
{
    public class McpServer : IDisposable
    {
        private readonly HttpListener _listener;
        private readonly SessionManager _sessionManager;
        private CancellationTokenSource? _cts;
        private Task? _listenTask;
        private bool _disposed;
        private static readonly JsonSerializerOptions JsonOpts = new()
        {
            PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
            WriteIndented = false,
        };

        public int Port { get; }
        public SessionManager SessionManager => _sessionManager;

        public McpServer(int port = 9223)
        {
            Port = port;
            _listener = new HttpListener();
            _listener.Prefixes.Add($"http://localhost:{Port}/");
            _sessionManager = new SessionManager();
        }

        public void Start()
        {
            try
            {
                _listener.Start();
            }
            catch (HttpListenerException ex)
            {
                throw new InvalidOperationException(
                    $"Failed to start HTTP server on port {Port}. " +
                    $"Try: netsh http add urlacl url=http://localhost:{Port}/ user=Everyone", ex);
            }

            _cts = new CancellationTokenSource();
            var ct = _cts.Token;
            ct.Register(() => _listener.Stop());

            _listenTask = Task.Run(() => ListenLoop(ct), ct);
        }

        public void Stop()
        {
            _cts?.Cancel();
        }

        private async Task ListenLoop(CancellationToken ct)
        {
            while (!ct.IsCancellationRequested)
            {
                try
                {
                    var context = await _listener.GetContextAsync();
                    _ = HandleRequestAsync(context, ct);
                }
                catch (ObjectDisposedException)
                {
                    break;
                }
                catch (HttpListenerException) when (ct.IsCancellationRequested)
                {
                    break;
                }
                catch (HttpListenerException)
                {
                    await Task.Delay(100, ct);
                }
            }
        }

        private async Task HandleRequestAsync(HttpListenerContext context, CancellationToken ct)
        {
            var request = context.Request;
            var response = context.Response;

            try
            {
                var path = request.Url?.AbsolutePath?.TrimEnd('/') ?? "";
                var method = request.HttpMethod;

                switch (method)
                {
                    case "GET" when path == "/health":
                        await WriteJsonAsync(response, 200, new { status = "ok", version = "1.0.0" });
                        return;

                    case "GET" when path == "/mcp/sessions":
                        var sessions = _sessionManager.ListSessions();
                        await WriteJsonAsync(response, 200, sessions);
                        return;

                    case "GET" when path == "/mcp/stream":
                        var sessionId = request.QueryString["sessionId"];
                        var since = request.QueryString["since"];

                        if (string.IsNullOrEmpty(sessionId))
                        {
                            await WriteJsonErrorAsync(response, 400, -32602, "Missing sessionId");
                            return;
                        }

                        var stream = _sessionManager.GetEventStream(sessionId);
                        if (stream == null)
                        {
                            await WriteJsonErrorAsync(response, 404, -32000, "Session not found");
                            return;
                        }

                        response.ContentType = "text/event-stream";
                        response.Headers["Cache-Control"] = "no-cache";
                        response.Headers["Connection"] = "keep-alive";
                        response.StatusCode = 200;

                        await stream.WriteToStreamAsync(response.OutputStream, since, ct);
                        return;

                    case "DELETE" when path.StartsWith("/mcp/sessions/"):
                        var id = path.Substring("/mcp/sessions/".Length);
                        var closed = await _sessionManager.CloseSessionAsync(id);
                        if (closed)
                            await WriteJsonAsync(response, 200, new { success = true });
                        else
                            await WriteJsonErrorAsync(response, 404, -32000, "Session not found");
                        return;

                    case "POST" when path == "/mcp/call":
                        await HandleJsonRpcAsync(request, response, ct);
                        return;

                    default:
                        await WriteJsonErrorAsync(response, 404, -32600, "Not Found");
                        return;
                }
            }
            catch (OperationCanceledException)
            {
                // Client disconnected
            }
            catch (Exception ex)
            {
                try
                {
                    await WriteJsonErrorAsync(response, 500, -32603, ex.Message);
                }
                catch
                {
                }
            }
            finally
            {
                try { response.OutputStream.Close(); } catch { }
            }
        }

        private async Task HandleJsonRpcAsync(HttpListenerRequest request,
            HttpListenerResponse response, CancellationToken ct)
        {
            string body;
            using (var reader = new StreamReader(request.InputStream, Encoding.UTF8))
            {
                body = await reader.ReadToEndAsync();
            }

            JsonRpcRequest? rpcRequest;
            try
            {
                rpcRequest = JsonSerializer.Deserialize<JsonRpcRequest>(body, JsonOpts);
            }
            catch (JsonException)
            {
                await WriteJsonRpcResponseAsync(response, null, null,
                    new JsonRpcError { Code = -32600, Message = "Invalid JSON" });
                return;
            }

            if (rpcRequest == null || rpcRequest.Method == null)
            {
                await WriteJsonRpcResponseAsync(response, null, null,
                    new JsonRpcError { Code = -32600, Message = "Invalid Request" });
                return;
            }

            try
            {
                await RouteJsonRpcAsync(rpcRequest, response, ct);
            }
            catch (Exception ex)
            {
                await WriteJsonRpcResponseAsync(response, rpcRequest.Id, null,
                    new JsonRpcError { Code = -32603, Message = ex.Message });
            }
        }

        private async Task RouteJsonRpcAsync(JsonRpcRequest rpcRequest,
            HttpListenerResponse response, CancellationToken ct)
        {
            if (rpcRequest.Method == "tools/call")
            {
                var rpcParams = rpcRequest.Params.HasValue
                    ? JsonSerializer.Deserialize<ToolsCallParams>(rpcRequest.Params.Value.GetRawText(), JsonOpts)
                    : null;

                if (rpcParams == null || string.IsNullOrEmpty(rpcParams.Name))
                {
                    await WriteJsonRpcResponseAsync(response, rpcRequest.Id, null,
                        new JsonRpcError { Code = -32602, Message = "Missing tool name" });
                    return;
                }

                var args = rpcParams.Arguments;
                var toolName = rpcParams.Name;

                object? result = toolName switch
                {
                    "web_session_create" => await HandleWebSessionCreateAsync(args),
                    "page_open" => await HandlePageOpenAsync(args),
                    "script_run" => await HandleScriptRunAsync(args),
                    "page_screenshot" => await HandlePageScreenshotAsync(args),
                    "web_inject" => await HandleWebInjectAsync(args),
                    "web_wait" => await HandleWebWaitAsync(args),
                    "web_set_interactive" => await HandleWebSetInteractiveAsync(args),
                    "web_capabilities" => HandleWebCapabilities(),
                    "web_session_close" => await HandleWebSessionCloseAsync(args),
                    "web_dialog_response" => await HandleWebDialogResponseAsync(args),
                    _ => new JsonRpcError { Code = -32601, Message = $"Unknown tool: {toolName}" }
                };

                if (result is JsonRpcError err)
                {
                    await WriteJsonRpcResponseAsync(response, rpcRequest.Id, null, err);
                }
                else
                {
                    await WriteJsonRpcResponseAsync(response, rpcRequest.Id, result, null);
                }
                return;
            }

            await WriteJsonRpcResponseAsync(response, rpcRequest.Id, null,
                new JsonRpcError { Code = -32601, Message = $"Unknown method: {rpcRequest.Method}" });
        }

        private async Task<object> HandleWebSessionCreateAsync(JsonElement? args)
        {
            int width = 1920, height = 1080;
            if (args.HasValue)
            {
                var a = args.Value;
                if (a.TryGetProperty("viewport", out var vp))
                {
                    if (vp.TryGetProperty("width", out var w)) width = w.GetInt32();
                    if (vp.TryGetProperty("height", out var h)) height = h.GetInt32();
                }
            }

            var info = await _sessionManager.CreateSessionAsync(width, height);
            return new { success = true, sessionId = info.SessionId };
        }

        private async Task<object> HandlePageOpenAsync(JsonElement? args)
        {
            var (sessionId, url) = ExtractSessionAndArg(args);
            if (sessionId == null) return Err("Missing or invalid sessionId");

            var session = _sessionManager.GetSession(sessionId);
            if (session == null) return SessionNotFoundErr();

            if (string.IsNullOrEmpty(url)) return Err("Missing url");

            await session.NavigateAsync(url);
            await Task.Delay(1000); // Brief wait for navigation to start

            return new
            {
                success = true,
                url,
                title = session.CurrentTitle,
                status = "complete"
            };
        }

        private async Task<object> HandleScriptRunAsync(JsonElement? args)
        {
            var (sessionId, _) = ExtractSessionAndArg(args);
            if (sessionId == null) return Err("Missing or invalid sessionId");

            var session = _sessionManager.GetSession(sessionId);
            if (session == null) return SessionNotFoundErr();

            string? script = null;
            if (args.HasValue)
            {
                var a = args.Value;
                if (a.TryGetProperty("script", out var s)) script = s.GetString();
            }

            if (string.IsNullOrEmpty(script)) return Err("Missing script");

            var timeoutMs = 10000;
            if (args.HasValue && args.Value.TryGetProperty("timeout", out var to))
                timeoutMs = to.GetInt32();

            using var cts = new CancellationTokenSource(timeoutMs);
            var result = await Task.Run(async () => await session.ExecuteScriptAsync(script),
                cts.Token);

            return new { success = true, value = result };
        }

        private async Task<object> HandlePageScreenshotAsync(JsonElement? args)
        {
            var (sessionId, _) = ExtractSessionAndArg(args);
            if (sessionId == null) return Err("Missing or invalid sessionId");

            var session = _sessionManager.GetSession(sessionId);
            if (session == null) return SessionNotFoundErr();

            var data = await session.TakeScreenshotAsync();
            return new { success = true, data, mimeType = "image/png" };
        }

        private async Task<object> HandleWebInjectAsync(JsonElement? args)
        {
            var (sessionId, _) = ExtractSessionAndArg(args);
            if (sessionId == null) return Err("Missing or invalid sessionId");

            var session = _sessionManager.GetSession(sessionId);
            if (session == null) return SessionNotFoundErr();

            string? css = null, js = null;
            if (args.HasValue)
            {
                var a = args.Value;
                if (a.TryGetProperty("css", out var c)) css = c.GetString();
                if (a.TryGetProperty("js", out var j)) js = j.GetString();
            }

            await session.InjectContentAsync(css, js);
            return new { success = true };
        }

        private async Task<object> HandleWebWaitAsync(JsonElement? args)
        {
            var (sessionId, _) = ExtractSessionAndArg(args);
            if (sessionId == null) return Err("Missing or invalid sessionId");

            var session = _sessionManager.GetSession(sessionId);
            if (session == null) return SessionNotFoundErr();

            string? selector = null;
            int timeout = 30000;
            if (args.HasValue)
            {
                var a = args.Value;
                if (a.TryGetProperty("selector", out var s)) selector = s.GetString();
                if (a.TryGetProperty("timeout", out var t)) timeout = t.GetInt32();
            }

            if (string.IsNullOrEmpty(selector)) return Err("Missing selector");

            var found = await session.WaitForElementAsync(selector, timeout);
            return new { success = found, found };
        }

        private async Task<object> HandleWebSetInteractiveAsync(JsonElement? args)
        {
            var (sessionId, _) = ExtractSessionAndArg(args);
            if (sessionId == null) return Err("Missing or invalid sessionId");

            var session = _sessionManager.GetSession(sessionId);
            if (session == null) return SessionNotFoundErr();

            bool interactive = false;
            if (args.HasValue && args.Value.TryGetProperty("interactive", out var i))
                interactive = i.GetBoolean();

            await session.SetInteractiveAsync(interactive);
            return new { success = true, interactive };
        }

        private static object HandleWebCapabilities()
        {
            return new
            {
                success = true,
                capabilities = new
                {
                    platform = "windows",
                    engine = "webview2",
                    features = new[]
                    {
                        "navigation",
                        "script_execution",
                        "screenshot",
                        "interactive_mode",
                        "dialog_capture",
                        "console_capture",
                        "content_injection",
                        "element_wait",
                    },
                    version = "1.0.0"
                }
            };
        }

        private async Task<object> HandleWebSessionCloseAsync(JsonElement? args)
        {
            var (sessionId, _) = ExtractSessionAndArg(args);
            if (sessionId == null) return Err("Missing or invalid sessionId");

            var closed = await _sessionManager.CloseSessionAsync(sessionId);
            return new { success = closed };
        }

        private Task<object> HandleWebDialogResponseAsync(JsonElement? args)
        {
            if (!args.HasValue) return Task.FromResult<object>(Err("Missing arguments"));

            var a = args.Value;
            string? sessionId = null, dialogId = null, action = null, text = null;

            if (a.TryGetProperty("sessionId", out var s)) sessionId = s.GetString();
            if (a.TryGetProperty("dialogId", out var d)) dialogId = d.GetString();
            if (a.TryGetProperty("action", out var ac)) action = ac.GetString();
            if (a.TryGetProperty("text", out var t)) text = t.GetString();

            if (sessionId == null || dialogId == null || action == null)
                return Task.FromResult<object>(Err("Missing sessionId, dialogId, or action"));

            var resolved = _sessionManager.ResolveDialog(sessionId, dialogId, action, text);
            return Task.FromResult<object>(new { success = resolved });
        }

        private static (string? sessionId, string? url) ExtractSessionAndArg(JsonElement? args)
        {
            if (!args.HasValue) return (null, null);

            var a = args.Value;
            string? sessionId = null, url = null;

            if (a.TryGetProperty("sessionId", out var s)) sessionId = s.GetString();
            if (a.TryGetProperty("url", out var u)) url = u.GetString();

            return (sessionId, url);
        }

        private static JsonRpcError Err(string message)
        {
            return new JsonRpcError { Code = -32602, Message = message };
        }

        private static JsonRpcError SessionNotFoundErr()
        {
            return new JsonRpcError { Code = -32000, Message = "Session not found" };
        }

        private static async Task WriteJsonAsync(HttpListenerResponse response,
            int statusCode, object data)
        {
            response.StatusCode = statusCode;
            response.ContentType = "application/json";
            var json = JsonSerializer.Serialize(data, JsonOpts);
            var bytes = Encoding.UTF8.GetBytes(json);
            response.ContentLength64 = bytes.Length;
            await response.OutputStream.WriteAsync(bytes, 0, bytes.Length);
        }

        private static async Task WriteJsonErrorAsync(HttpListenerResponse response,
            int statusCode, int errorCode, string message)
        {
            await WriteJsonAsync(response, statusCode, new
            {
                error = new { code = errorCode, message }
            });
        }

        private static async Task WriteJsonRpcResponseAsync(
            HttpListenerResponse response,
            JsonElement? id,
            object? result,
            JsonRpcError? error)
        {
            response.StatusCode = 200;
            response.ContentType = "application/json";

            var rpcResponse = new
            {
                jsonrpc = "2.0",
                id = id?.ToString() ?? null,
                result,
                error = error == null ? null : new { error.Code, error.Message }
            };

            var json = JsonSerializer.Serialize(rpcResponse, JsonOpts);
            var bytes = Encoding.UTF8.GetBytes(json);
            response.ContentLength64 = bytes.Length;
            await response.OutputStream.WriteAsync(bytes, 0, bytes.Length);
        }

        public void Dispose()
        {
            if (_disposed) return;
            _disposed = true;
            Stop();
            _sessionManager.Dispose();
            (_listener as IDisposable)?.Dispose();
        }
    }
}
