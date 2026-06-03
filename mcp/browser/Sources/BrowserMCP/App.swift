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

    func applicationDidFinishLaunching(_ notification: Notification) {
        setupMenuBar()
        NSApp.setActivationPolicy(.regular)
        NSApp.applicationIconImage = generateAppIcon()

        registerURLHandler()

        // Start HTTP MCP server
        do {
            httpServer = MCPHttpServer(viewModel: viewModel, port: 9876)
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

        // Debug: write to file to confirm setupMenuBar ran
        try? "setupMenuBar ran at \(Date())\n".write(toFile: "/tmp/browser-mcp-debug.log", atomically: true, encoding: .utf8)
    }

    @objc private func showWindowAction() {
        showWindow()
    }

    @objc private func showSettingsAction() {
        if #available(macOS 14, *) {
            NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
        } else {
            NSApp.sendAction(Selector(("showPreferencesWindow:")), to: nil, from: nil)
        }
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
