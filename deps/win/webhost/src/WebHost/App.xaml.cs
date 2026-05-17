using System;
using System.ComponentModel;
using System.Windows;
using System.Windows.Threading;

namespace Dolphin.WebHost
{
    public partial class App : Application
    {
        private McpServer? _server;
        private MainWindow? _mainWindow;

        protected override void OnStartup(StartupEventArgs e)
        {
            base.OnStartup(e);

            _mainWindow = new MainWindow();

            try
            {
                _server = new McpServer(port: 9223);
                _server.Start();
                _mainWindow.SetStatus($"WebHost running on http://localhost:9223");
            }
            catch (Exception ex)
            {
                _mainWindow.SetStatus($"Failed to start: {ex.Message}");
                MessageBox.Show($"Failed to start WebHost server: {ex.Message}",
                    "Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }

            DispatcherUnhandledException += OnDispatcherUnhandledException;
        }

        protected override void OnExit(ExitEventArgs e)
        {
            _server?.Stop();
            _server?.Dispose();
            base.OnExit(e);
        }

        private void OnDispatcherUnhandledException(object sender,
            DispatcherUnhandledExceptionEventArgs e)
        {
            MessageBox.Show($"Unhandled error: {e.Exception.Message}",
                "Error", MessageBoxButton.OK, MessageBoxImage.Error);
            e.Handled = true;
        }
    }
}
