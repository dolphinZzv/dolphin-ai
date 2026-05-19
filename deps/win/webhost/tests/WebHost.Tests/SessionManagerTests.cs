using System.Linq;
using System.Threading.Tasks;
using Xunit;

namespace Dolphin.WebHost.Tests
{
    public class SessionManagerTests
    {
        [Fact]
        public void New_Manager_Has_No_Sessions()
        {
            using var mgr = new SessionManager();
            var sessions = mgr.ListSessions();
            Assert.Empty(sessions);
        }

        [Fact]
        public void GetSession_Returns_Null_For_Unknown()
        {
            using var mgr = new SessionManager();
            var session = mgr.GetSession("nonexistent");
            Assert.Null(session);
        }

        [Fact]
        public void GetEventStream_Returns_Null_For_Unknown()
        {
            using var mgr = new SessionManager();
            var stream = mgr.GetEventStream("nonexistent");
            Assert.Null(stream);
        }

        [Fact]
        public async Task CloseSession_Returns_False_For_Unknown()
        {
            using var mgr = new SessionManager();
            var closed = await mgr.CloseSessionAsync("nonexistent");
            Assert.False(closed);
        }

        [Fact]
        public void ResolveDialog_Returns_False_For_Unknown()
        {
            using var mgr = new SessionManager();
            var resolved = mgr.ResolveDialog("nonexistent", "dlg_1", "accept");
            Assert.False(resolved);
        }

        [Fact]
        public void ListSessions_Is_Thread_Safe()
        {
            using var mgr = new SessionManager();
            var tasks = Enumerable.Range(0, 100).Select(_ => Task.Run(() => mgr.ListSessions()));
            var allResults = Task.WhenAll(tasks).GetAwaiter().GetResult();
            Assert.All(allResults, list => Assert.NotNull(list));
        }

        [Fact]
        public void Dispose_Does_Not_Throw()
        {
            var mgr = new SessionManager();
            mgr.Dispose();
            mgr.Dispose();
        }
    }
}