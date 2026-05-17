using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.IO;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;

namespace Dolphin.WebHost
{
    public class EventStream
    {
        private readonly ConcurrentQueue<EventItem> _queue = new();
        private readonly SemaphoreSlim _signal = new(0);
        private long _lastTimestamp;
        private const int MaxQueueSize = 10000;

        public long LastTimestamp => Interlocked.Read(ref _lastTimestamp);

        public void Publish(string method, string? paramsJson = null)
        {
            var timestamp = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
            Interlocked.Exchange(ref _lastTimestamp, timestamp);

            string json;
            if (!string.IsNullOrEmpty(paramsJson))
            {
                try
                {
                    using var doc = JsonDocument.Parse(paramsJson);
                    using var stream = new MemoryStream();
                    using var writer = new Utf8JsonWriter(stream);
                    writer.WriteStartObject();
                    writer.WriteString("jsonrpc", "2.0");
                    writer.WriteString("method", method);
                    writer.WriteStartObject("params");
                    writer.WriteNumber("t", timestamp);
                    foreach (var prop in doc.RootElement.EnumerateObject())
                    {
                        prop.Value.WriteTo(writer);
                    }
                    writer.WriteEndObject();
                    writer.WriteEndObject();
                    writer.Flush();
                    json = Encoding.UTF8.GetString(stream.ToArray());
                }
                catch
                {
                    json = $"{{\"jsonrpc\":\"2.0\",\"method\":\"{EscapeJson(method)}\",\"params\":{{\"t\":{timestamp},\"raw\":{paramsJson}}}}}";
                }
            }
            else
            {
                json = $"{{\"jsonrpc\":\"2.0\",\"method\":\"{EscapeJson(method)}\",\"params\":{{\"t\":{timestamp}}}}}";
            }

            if (_queue.Count >= MaxQueueSize)
            {
                _queue.TryDequeue(out _);
            }
            _queue.Enqueue(new EventItem(timestamp, json));
            _signal.Release();
        }

        private static string EscapeJson(string s)
        {
            return s.Replace("\\", "\\\\").Replace("\"", "\\\"");
        }

        public async Task WriteToStreamAsync(Stream responseStream, string? sinceStr,
            CancellationToken ct)
        {
            long since = 0;
            if (!string.IsNullOrEmpty(sinceStr) &&
                long.TryParse(sinceStr, out var parsed))
            {
                since = parsed;
            }

            using var writer = new StreamWriter(responseStream, Encoding.UTF8, 1024, leaveOpen: true);

            await writer.WriteAsync("data: {\"jsonrpc\":\"2.0\",\"method\":\"web/stream_connected\",\"params\":{\"t\":" +
                DateTimeOffset.UtcNow.ToUnixTimeSeconds() + ",\"since\":" + since + "}}\n\n");
            await writer.FlushAsync();

            var pendingEvents = new List<EventItem>();
            while (_queue.TryDequeue(out var item))
            {
                if (item.Timestamp > since)
                    pendingEvents.Add(item);
            }

            foreach (var item in pendingEvents)
            {
                await writer.WriteAsync("data: " + item.Json + "\n\n");
                await writer.FlushAsync();
            }

            var heartbeatCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            while (!ct.IsCancellationRequested)
            {
                var waitTask = _signal.WaitAsync(30000, heartbeatCts.Token);
                try
                {
                    var signaled = await waitTask;
                    if (!signaled)
                    {
                        await writer.WriteAsync("data: {\"jsonrpc\":\"2.0\",\"method\":\"web/ping\",\"params\":{\"t\":" +
                            DateTimeOffset.UtcNow.ToUnixTimeSeconds() + "}}\n\n");
                        await writer.FlushAsync();
                        continue;
                    }
                }
                catch (OperationCanceledException)
                {
                    break;
                }

                while (_queue.TryDequeue(out var item))
                {
                    await writer.WriteAsync("data: " + item.Json + "\n\n");
                    await writer.FlushAsync();
                }
            }
        }

        private readonly struct EventItem
        {
            public long Timestamp { get; }
            public string Json { get; }

            public EventItem(long timestamp, string json)
            {
                Timestamp = timestamp;
                Json = json;
            }
        }
    }
}
