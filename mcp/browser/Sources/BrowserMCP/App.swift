import AppKit
import SwiftUI
import BrowserMCPCore

@main
struct BrowserApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate

    var body: some Scene {
        WindowGroup {
            ContentView(viewModel: appDelegate.viewModel)
                .frame(minWidth: 640, minHeight: 480)
        }
        .windowResizability(.contentMinSize)
        .commands {
            CommandGroup(replacing: .newItem) { }
        }

        Settings {
            SettingsView(viewModel: appDelegate.viewModel)
        }
    }
}

@MainActor
class AppDelegate: NSObject, NSApplicationDelegate {
    let viewModel = WebViewModel()
    var httpServer: MCPHttpServer?
    private var statusItem: NSStatusItem?
    private var settingsWindowController: NSWindowController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        setupMenuBar()
        NSApp.setActivationPolicy(.regular)
        NSApp.applicationIconImage = generateAppIcon()

        registerURLHandler()

        // Start HTTP MCP server
        do {
            let port = ProcessInfo.processInfo.environment["BROWSER_MCP_PORT"].flatMap(UInt16.init) ?? 9876
            httpServer = MCPHttpServer(viewModel: viewModel, port: port)
            try httpServer?.start()
        } catch {
            print("[MCP] HTTP server failed: \(error)")
        }
    }

    func showWindow() {
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first {
            window.makeKeyAndOrderFront(nil)
            viewModel.applyWindowSize()
        }
    }

    // MARK: - Menu bar

    private func setupMenuBar() {
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.title = "MCP"
        item.button?.font = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .medium)

        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "Show Window", action: #selector(showWindowAction), keyEquivalent: ""))
        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: "Settings...", action: #selector(showSettingsAction), keyEquivalent: ","))
        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))
        item.menu = menu
        statusItem = item
    }

    @objc private func showWindowAction() {
        showWindow()
    }

    @objc private func showSettingsAction() {
        NSApp.activate(ignoringOtherApps: true)

        // Reuse existing settings window if still open
        if let wc = settingsWindowController, let window = wc.window, window.isVisible {
            window.makeKeyAndOrderFront(nil)
            return
        }

        let settingsView = SettingsView(viewModel: viewModel)
        let hostingController = NSHostingController(rootView: settingsView)
        let window = NSWindow(contentViewController: hostingController)
        window.title = "Settings"
        window.styleMask = [.titled, .closable, .miniaturizable]

        let wc = NSWindowController(window: window)
        wc.showWindow(nil)
        settingsWindowController = wc
    }

    // MARK: - URL handler (browser-mcp:// urls)

    private func registerURLHandler() {
        NSAppleEventManager.shared().setEventHandler(
            self,
            andSelector: #selector(handleURLEvent(_:withReply:)),
            forEventClass: AEEventClass(kInternetEventClass),
            andEventID: AEEventID(kAEGetURL)
        )
    }

    @objc private func handleURLEvent(_ event: NSAppleEventDescriptor, withReply reply: NSAppleEventDescriptor) {
        if let urlStr = event.paramDescriptor(forKeyword: keyDirectObject)?.stringValue,
           let components = URLComponents(string: urlStr),
           components.scheme == "browser-mcp",
           let command = components.host {
            switch command {
            case "show":
                showWindow()
            case "navigate":
                if let url = components.queryItems?.first(where: { $0.name == "url" })?.value {
                    Task { try? await viewModel.navigate(to: url) }
                    showWindow()
                }
            default:
                break
            }
        }
    }
}
