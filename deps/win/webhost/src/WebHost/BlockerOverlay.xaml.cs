using System.Windows;
using System.Windows.Input;

namespace Dolphin.WebHost
{
    public partial class BlockerOverlay : Window
    {
        private Window? _target;

        public BlockerOverlay()
        {
            InitializeComponent();
            PreviewKeyDown += OnPreviewKeyDown;
            PreviewMouseDown += OnPreviewMouseDown;
            PreviewMouseWheel += OnPreviewMouseWheel;
        }

        public void AttachTo(Window target)
        {
            _target = target;

            Left = target.Left;
            Top = target.Top;
            Width = target.Width;
            Height = target.Height;

            target.LocationChanged += (_, _) => SyncPosition();
            target.SizeChanged += (_, _) => SyncPosition();

            Owner = target;
        }

        private void SyncPosition()
        {
            if (_target == null) return;
            Left = _target.Left;
            Top = _target.Top;
            Width = _target.Width;
            Height = _target.Height;
        }

        public void ShowOverlay()
        {
            if (Dispatcher.CheckAccess())
            {
                Show();
            }
            else
            {
                Dispatcher.Invoke(Show);
            }
        }

        public void HideOverlay()
        {
            if (Dispatcher.CheckAccess())
            {
                Hide();
            }
            else
            {
                Dispatcher.Invoke(Hide);
            }
        }

        private void OnPreviewKeyDown(object sender, KeyEventArgs e)
        {
            e.Handled = true;
        }

        private void OnPreviewMouseDown(object sender, MouseButtonEventArgs e)
        {
            e.Handled = true;
        }

        private void OnPreviewMouseWheel(object sender, MouseWheelEventArgs e)
        {
            e.Handled = true;
        }
    }
}
