using System;
using System.Diagnostics;
using System.IO;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Threading;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.Wpf;

namespace Dolphin.WebHost
{
    public class WebView2Session : IDisposable
    {
        private readonly string _sessionId;
        private readonly EventStream _eventStream;
        private Window? _window;
        private WebView2? _webView;
        private BlockerOverlay? _overlay;
        private TaskCompletionSource<bool>? _initTcs;
        private DispatcherTimer? _pollTimer;
        private DispatcherTimer? _pingTimer;
        private Uri? _lastSource;
        private bool _disposed;

        public string SessionId => _sessionId;
        public bool IsInitialized { get; private set; }
        public bool IsInteractive { get; private set; }
        private string _currentUrl = "";
        private string _currentTitle = "";
        public string CurrentUrl => _currentUrl;
        public string CurrentTitle => _currentTitle;
        public DateTime CreatedAt { get; } = DateTime.UtcNow;
        public DateTime LastActivityAt { get; private set; } = DateTime.UtcNow;

        private const string InjectScript = @"
(function() {
    if (window.__webhostInjected) return;
    window.__webhostInjected = true;
    window.__webhost = window.__webhost || {};
    window.__webhost.consoleLogs = [];
    window.__webhost.dialogs = [];
    window.__webhost.dialogResolve = {};
    window.__webhost.pendingDialog = null;

    var orig = {};
    ['log','warn','error','info','debug'].forEach(function(m) {
        orig[m] = console[m];
        console[m] = function() {
            var args = Array.prototype.slice.call(arguments);
            window.__webhost.consoleLogs.push({
                level: m,
                message: args.map(function(a) {
                    try { return typeof a === 'string' ? a : JSON.stringify(a); }
                    catch(e) { return String(a); }
                }).join(' '),
                timestamp: Date.now()
            });
            orig[m].apply(console, arguments);
        };
    });

    window.alert = function(msg) {
        var id = 'dlg_' + Date.now() + '_' + Math.random().toString(36).substr(2,9);
        window.__webhost.pendingDialog = {id:id, type:'alert', message:String(msg), defaultValue:''};
        window.__webhost.dialogs.push(window.__webhost.pendingDialog);
        return new Promise(function(resolve) {
            window.__webhost.dialogResolve[id] = function(action, text) {
                resolve();
                window.__webhost.pendingDialog = null;
                delete window.__webhost.dialogResolve[id];
            };
        });
    };

    window.confirm = function(msg) {
        var id = 'dlg_' + Date.now() + '_' + Math.random().toString(36).substr(2,9);
        window.__webhost.pendingDialog = {id:id, type:'confirm', message:String(msg), defaultValue:''};
        window.__webhost.dialogs.push(window.__webhost.pendingDialog);
        return new Promise(function(resolve) {
            window.__webhost.dialogResolve[id] = function(action, text) {
                resolve(action === 'accept');
                window.__webhost.pendingDialog = null;
                delete window.__webhost.dialogResolve[id];
            };
        });
    };

    window.prompt = function(msg, def) {
        var id = 'dlg_' + Date.now() + '_' + Math.random().toString(36).substr(2,9);
        window.__webhost.pendingDialog = {id:id, type:'prompt', message:String(msg), defaultValue:String(def||'')};
        window.__webhost.dialogs.push(window.__webhost.pendingDialog);
        return new Promise(function(resolve) {
            window.__webhost.dialogResolve[id] = function(action, text) {
                resolve(action === 'accept' ? (text || '') : null);
                window.__webhost.pendingDialog = null;
                delete window.__webhost.dialogResolve[id];
            };
        });
    };

    window.__webhost.elementExists = function(selector) {
        return document.querySelector(selector) !== null;
    };
})();
";

        public WebView2Session(string sessionId, EventStream eventStream)
        {
            _sessionId = sessionId;
            _eventStream = eventStream;
        }

        private int _initCalled;

        public async Task InitializeAsync(int viewportWidth = 1920, int viewportHeight = 1080)
        {
            if (Interlocked.Exchange(ref _initCalled, 1) != 0)
                throw new InvalidOperationException("InitializeAsync already called");

            _initTcs = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);

            if (!Application.Current.Dispatcher.CheckAccess())
            {
                await Application.Current.Dispatcher.InvokeAsync(
                    () => InitializeOnUIThread(viewportWidth, viewportHeight)).Task.Unwrap();
            }
            else
            {
                await InitializeOnUIThread(viewportWidth, viewportHeight);
            }

            await _initTcs.Task;
        }

        private async Task InitializeOnUIThread(int viewportWidth, int viewportHeight)
        {
            try
            {
                var userDataFolder = System.IO.Path.Combine(
                    System.IO.Path.GetTempPath(), "DolphinWebHost", _sessionId);
                Directory.CreateDirectory(userDataFolder);

                _window = new Window
                {
                    Width = viewportWidth,
                    Height = viewportHeight,
                    WindowStyle = WindowStyle.SingleBorderWindow,
                    ResizeMode = ResizeMode.CanResize,
                    ShowInTaskbar = false,
                    WindowState = WindowState.Normal,
                    Title = $"WebHost {_sessionId}",
                };

                _webView = new WebView2();
                _webView.CreationProperties = new CoreWebView2CreationProperties
                {
                    UserDataFolder = userDataFolder
                };
                _window.Content = _webView;

                _window.Show();
                _window.WindowState = WindowState.Minimized;
                _window.Left = -9999;
                _window.Top = -9999;
                _window.ShowInTaskbar = false;

                await _webView.EnsureCoreWebView2Async();

                _overlay = new BlockerOverlay();
                _overlay.AttachTo(_window);

                _lastSource = _webView.Source;
                _currentUrl = _webView.Source?.ToString() ?? "";
                _currentTitle = _webView.CoreWebView2?.DocumentTitle ?? "";

                await _webView.ExecuteScriptAsync(InjectScript);

                _pollTimer = new DispatcherTimer(
                    TimeSpan.FromMilliseconds(300), DispatcherPriority.Background,
                    OnPollTimerTick, Application.Current.Dispatcher);

                _pingTimer = new DispatcherTimer(
                    TimeSpan.FromSeconds(30), DispatcherPriority.Background,
                    (_, _) => _eventStream.Publish("web/ping"), Application.Current.Dispatcher);

                _pollTimer.Start();
                _pingTimer.Start();

                IsInitialized = true;
                _initTcs?.TrySetResult(true);
            }
            catch (Exception ex)
            {
                _initTcs?.TrySetException(ex);
            }
        }

        private async void OnPollTimerTick(object? sender, EventArgs e)
        {
            if (_webView?.Source == null || _disposed) return;

            try
            {
                var currentSource = _webView.Source?.ToString() ?? "";
                var currentTitle = _webView.CoreWebView2?.DocumentTitle ?? "";
                _currentUrl = currentSource;
                _currentTitle = currentTitle;
                if (_lastSource == null || currentSource != _lastSource.ToString())
                {
                    _lastSource = new Uri(currentSource);
                    _eventStream.Publish("web/navigation",
                        $"{{\"url\":{EscapeJson(currentSource)},\"title\":{EscapeJson(currentTitle)},\"status\":\"complete\"}}");
                }

                var consoleJson = await _webView.ExecuteScriptAsync(
                    @"JSON.stringify((function(){
                        var result = [];
                        var item;
                        while(item = window.__webhost.consoleLogs.shift()) result.push(item);
                        return result;
                    })())");

                if (!string.IsNullOrEmpty(consoleJson) && consoleJson != "[]" && consoleJson != "null")
                {
                    try
                    {
                        using var doc = JsonDocument.Parse(consoleJson);
                        foreach (var item in doc.RootElement.EnumerateArray())
                        {
                            var msg = item.TryGetProperty("message", out var m) ? m.GetString() : "";
                            var level = item.TryGetProperty("level", out var l) ? l.GetString() : "log";
                            _eventStream.Publish("web/console",
                                $"{{\"level\":{EscapeJson(level)},\"message\":{EscapeJson(msg ?? "")}}}");
                        }
                    }
                    catch (JsonException ex)
                    {
                        Logger.Warn($"Console parse error: {ex.Message}");
                    }
                }

                var dialogJson = await _webView.ExecuteScriptAsync(
                    @"JSON.stringify((function(){
                        var item = window.__webhost.dialogs.shift();
                        return item || null;
                    })())");

                if (!string.IsNullOrEmpty(dialogJson) && dialogJson != "null")
                {
                    _eventStream.Publish("web/dialog", dialogJson);
                }

                UpdateActivity();
            }
            catch (Exception ex)
            {
                Logger.Warn($"Poll tick error: {ex.Message}");
            }
        }

        public async Task NavigateAsync(string url)
        {
            ThrowIfNotInitialized();
            await DispatchAsync(() =>
            {
                _webView!.Source = new Uri(url);
                _lastSource = _webView!.Source;
            });
            _eventStream.Publish("web/navigation",
                $"{{\"url\":{EscapeJson(url)},\"status\":\"starting\"}}");
            UpdateActivity();
        }

        public async Task<string> ExecuteScriptAsync(string script)
        {
            ThrowIfNotInitialized();
            UpdateActivity();
            return await DispatchAsync(async () =>
            {
                return await _webView!.ExecuteScriptAsync(script) ?? "";
            });
        }

        public async Task<string> TakeScreenshotAsync()
        {
            ThrowIfNotInitialized();
            if (_disposed) return "";
            UpdateActivity();

            return await DispatchAsync(async () =>
            {
                using var ms = new MemoryStream();
                await _webView!.CoreWebView2.CapturePreviewAsync(
                    CoreWebView2CapturePreviewImageFormat.Png, ms);
                ms.Position = 0;
                return Convert.ToBase64String(ms.ToArray());
            });
        }

        public async Task SetInteractiveAsync(bool interactive)
        {
            ThrowIfNotInitialized();
            await DispatchAsync(() =>
            {
                IsInteractive = interactive;
                if (interactive)
                {
                    _window!.WindowState = WindowState.Normal;
                    _window.Left = (SystemParameters.PrimaryScreenWidth - _window.Width) / 2;
                    _window.Top = (SystemParameters.PrimaryScreenHeight - _window.Height) / 2;
                    _window.ShowInTaskbar = true;
                    _window.Activate();
                    _overlay?.HideOverlay();
                }
                else
                {
                    _window!.WindowState = WindowState.Minimized;
                    _window.Left = -9999;
                    _window.Top = -9999;
                    _window.ShowInTaskbar = false;
                    _overlay?.ShowOverlay();
                }
                UpdateActivity();
            });
        }

        public async Task InjectContentAsync(string? css, string? js)
        {
            ThrowIfNotInitialized();
            await DispatchAsync(async () =>
            {
                if (!string.IsNullOrEmpty(css))
                {
                    var escaped = css.Replace("\\", "\\\\").Replace("'", "\\'").Replace("\n", "\\n");
                    await _webView!.ExecuteScriptAsync(
                        $"(()=>{{var s=document.createElement('style');s.textContent='{escaped}';document.head.appendChild(s);}})()");
                }
                if (!string.IsNullOrEmpty(js))
                {
                    await _webView!.ExecuteScriptAsync(js);
                }
            });
            UpdateActivity();
        }

        public async Task<bool> WaitForElementAsync(string selector, int timeoutMs = 30000)
        {
            ThrowIfNotInitialized();
            UpdateActivity();
            var escaped = selector.Replace("\\", "\\\\").Replace("'", "\\'");
            var sw = Stopwatch.StartNew();
            while (sw.ElapsedMilliseconds < timeoutMs)
            {
                var result = await DispatchAsync(async () =>
                    await _webView!.ExecuteScriptAsync(
                        $"window.__webhost.elementExists('{escaped}')"));
                if (result == "true") return true;
                await Task.Delay(100);
            }
            return false;
        }

        public async Task CloseAsync()
        {
            if (!IsInitialized) return;
            await DispatchAsync(() =>
            {
                _pollTimer?.Stop();
                _pingTimer?.Stop();
                _overlay?.Close();
                _window?.Close();
            });
            Dispose();
        }

        public bool ResolveDialog(string dialogId, string action, string? text = null)
        {
            var et = (text ?? "").Replace("\\", "\\\\").Replace("'", "\\'");
            var ei = dialogId.Replace("'", "\\'");
            _ = DispatchAsync(async () =>
            {
                await _webView!.ExecuteScriptAsync(
                    $"window.__webhost.dialogResolve['{ei}']('{action}','{et}')");
            });
            return true;
        }

        private static string EscapeJson(string? s)
        {
            if (s == null) return "null";
            return "\"" + s.Replace("\\", "\\\\").Replace("\"", "\\\"")
                .Replace("\n", "\\n").Replace("\r", "\\r").Replace("\t", "\\t") + "\"";
        }

        private void ThrowIfNotInitialized()
        {
            if (!IsInitialized)
                throw new InvalidOperationException($"Session {_sessionId} not initialized");
            if (_disposed)
                throw new ObjectDisposedException($"Session {_sessionId} has been disposed");
        }

        private void UpdateActivity() => LastActivityAt = DateTime.UtcNow;

        private Task DispatchAsync(Action action)
        {
            if (!Application.Current.Dispatcher.CheckAccess())
                return Application.Current.Dispatcher.InvokeAsync(action).Task;
            action();
            return Task.CompletedTask;
        }

        private Task<T> DispatchAsync<T>(Func<T> func)
        {
            if (!Application.Current.Dispatcher.CheckAccess())
                return Application.Current.Dispatcher.InvokeAsync(func).Task;
            return Task.FromResult(func());
        }

        private Task<T> DispatchAsync<T>(Func<Task<T>> asyncFunc)
        {
            if (!Application.Current.Dispatcher.CheckAccess())
                return Application.Current.Dispatcher.InvokeAsync(asyncFunc).Task.Unwrap();
            return asyncFunc();
        }

        public void Dispose()
        {
            if (_disposed) return;
            _disposed = true;
            try
            {
                _pollTimer?.Stop();
                _pingTimer?.Stop();
                _overlay?.Close();
                _overlay = null;
                _window?.Close();
                _window = null;
                _webView?.Dispose();
                _webView = null;
            }
            catch (Exception ex)
            {
                Logger.Warn($"Dispose error: {ex.Message}");
            }
        }

    }
}
