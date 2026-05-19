#nullable disable
using System;
using System.IO;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Xunit;

namespace Dolphin.WebHost.Tests
{
    public class EventStreamTests
    {
        [Fact]
        public void Publish_Starts_With_Zero_Timestamp()
        {
            var es = new EventStream();
            Assert.Equal(0, es.LastTimestamp);
        }

        [Fact]
        public void Publish_Updates_Timestamp()
        {
            var es = new EventStream();
            es.Publish("web/ping");
            Assert.True(es.LastTimestamp > 0);
        }

        [Fact]
        public void Publish_Without_Params_Produces_Valid_Json()
        {
            var es = new EventStream();
            es.Publish("web/test");

            var output = CaptureStream(es, null);
            Assert.Contains("\"jsonrpc\":\"2.0\"", output);
            Assert.Contains("\"method\":\"web/test\"", output);
            Assert.Contains("\"params\"", output);
        }

        [Fact]
        public void Publish_With_Params_Includes_Data()
        {
            var es = new EventStream();
            es.Publish("web/console", "{\"level\":\"info\",\"message\":\"hello\"}");

            var output = CaptureStream(es, null);
            Assert.Contains("\"level\":\"info\"", output);
            Assert.Contains("\"message\":\"hello\"", output);
        }

        [Fact]
        public void WriteToStream_Sends_Connected_Event()
        {
            var es = new EventStream();
            var output = CaptureStream(es, null);
            Assert.Contains("web/stream_connected", output);
        }

        [Fact]
        public void WriteToStream_Includes_Pending_Events()
        {
            var es = new EventStream();
            es.Publish("web/test", "{\"foo\":\"bar\"}");

            var output = CaptureStream(es, null);
            Assert.Contains("web/test", output);
            Assert.Contains("\"foo\":\"bar\"", output);
        }

        [Fact]
        public void WriteToStream_Since_Filter_Excludes_Older_Events()
        {
            var es = new EventStream();
            es.Publish("web/old_event");

            var ts = es.LastTimestamp;

            // Ensure new event has a strictly larger timestamp
            Task.Delay(5).GetAwaiter().GetResult();
            es.Publish("web/new_event");

            var output = CaptureStream(es, ts.ToString());
            Assert.DoesNotContain("web/old_event", output);
            Assert.Contains("web/new_event", output);
        }

        [Fact]
        public void Queue_Overflow_Does_Not_Throw()
        {
            var es = new EventStream();

            for (int i = 0; i < 10010; i++)
            {
                es.Publish("web/overflow");
            }

            var output = CaptureStream(es, null);
            Assert.Contains("web/overflow", output);
        }

        private static string CaptureStream(EventStream es, string since)
        {
            using var ms = new MemoryStream();
            using var cts = new CancellationTokenSource();

            var writeTask = Task.Run(async () =>
            {
                try
                {
                    await es.WriteToStreamAsync(ms, since, cts.Token);
                }
                catch (OperationCanceledException) { }
            });

            if (!writeTask.Wait(3000))
            {
                cts.Cancel();
                writeTask.Wait(1000);
            }
            else
            {
                cts.Cancel();
            }

            ms.Position = 0;
            return Encoding.UTF8.GetString(ms.ToArray());
        }
    }
}