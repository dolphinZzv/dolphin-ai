using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Dolphin.WebHost.Models;

namespace Dolphin.WebHost
{
    public class SessionManager : IDisposable
    {
        private readonly ConcurrentDictionary<string, WebView2Session> _sessions = new();
        private readonly ConcurrentDictionary<string, EventStream> _eventStreams = new();
        private readonly int _maxSessions;
        private readonly TimeSpan _idleTimeout;
        private bool _disposed;

        public SessionManager(int maxSessions = 10, TimeSpan? idleTimeout = null)
        {
            _maxSessions = maxSessions;
            _idleTimeout = idleTimeout ?? TimeSpan.FromMinutes(5);
        }

        public async Task<SessionInfo> CreateSessionAsync(int viewportWidth = 1920, int viewportHeight = 1080)
        {
            if (_sessions.Count >= _maxSessions)
                throw new InvalidOperationException("Session limit exceeded");

            var sessionId = GenerateSessionId();
            var eventStream = new EventStream();
            var session = new WebView2Session(sessionId, eventStream);

            await session.InitializeAsync(viewportWidth, viewportHeight);

            _sessions[sessionId] = session;
            _eventStreams[sessionId] = eventStream;

            return new SessionInfo
            {
                SessionId = sessionId,
                Url = "",
                Title = "",
                IsInteractive = false,
                CreatedAt = session.CreatedAt,
                LastActivityAt = session.LastActivityAt,
            };
        }

        public WebView2Session? GetSession(string sessionId)
        {
            _sessions.TryGetValue(sessionId, out var session);
            return session;
        }

        public EventStream? GetEventStream(string sessionId)
        {
            _eventStreams.TryGetValue(sessionId, out var stream);
            return stream;
        }

        public async Task<bool> CloseSessionAsync(string sessionId)
        {
            if (_sessions.TryRemove(sessionId, out var session))
            {
                _eventStreams.TryRemove(sessionId, out _);
                await session.CloseAsync();
                return true;
            }
            return false;
        }

        public List<SessionInfo> ListSessions()
        {
            return _sessions.Values.Select(s => new SessionInfo
            {
                SessionId = s.SessionId,
                Url = s.CurrentUrl,
                Title = s.CurrentTitle,
                IsInteractive = s.IsInteractive,
                CreatedAt = s.CreatedAt,
                LastActivityAt = s.LastActivityAt,
            }).ToList();
        }

        public async Task ReclaimIdleSessionsAsync()
        {
            var now = DateTime.UtcNow;
            var idle = _sessions.Values
                .Where(s => (now - s.LastActivityAt) > _idleTimeout)
                .ToList();

            foreach (var session in idle)
            {
                await CloseSessionAsync(session.SessionId);
            }
        }

        public bool ResolveDialog(string sessionId, string dialogId, string action, string? text = null)
        {
            var session = GetSession(sessionId);
            return session?.ResolveDialog(dialogId, action, text) ?? false;
        }

        private static string GenerateSessionId()
        {
            var bytes = new byte[4];
            using var rng = new System.Security.Cryptography.RNGCryptoServiceProvider();
            rng.GetBytes(bytes);
            return "sess_" + Convert.ToHexString(bytes).ToLower();
        }

        public void Dispose()
        {
            if (_disposed) return;
            _disposed = true;

            foreach (var session in _sessions.Values)
            {
                session.Dispose();
            }
            _sessions.Clear();
            _eventStreams.Clear();
        }
    }
}
