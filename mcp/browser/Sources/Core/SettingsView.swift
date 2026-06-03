import SwiftUI

// MARK: - SettingsView

public struct SettingsView: View {
    @ObservedObject public var viewModel: WebViewModel

    public init(viewModel: WebViewModel) {
        self.viewModel = viewModel
    }

    @AppStorage("user_agent") private var customUserAgent: String = ""
    @State private var editedUserAgent: String = ""
    @State private var editedWidth: String = ""
    @State private var editedHeight: String = ""
    @State private var saved = false

    public var body: some View {
        TabView {
            browserSettings
                .tabItem {
                    Label("Browser", systemImage: "globe")
                }
        }
        .frame(width: 480, height: 320)
        .onAppear {
            editedUserAgent = customUserAgent
            editedWidth = String(format: "%.0f", viewModel.windowWidth)
            editedHeight = String(format: "%.0f", viewModel.windowHeight)
        }
    }

    // MARK: - Browser settings

    private var browserSettings: some View {
        Form {
            userAgentSection
            Divider().padding(.vertical, 4)
            windowSizeSection
        }
        .padding()
    }

    // MARK: - User-Agent section

    private var userAgentSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("HTTP User-Agent")
                .font(.headline)

            Text("Customize the User-Agent header sent by the browser when loading web pages.")
                .font(.caption)
                .foregroundColor(.secondary)

            TextField("Custom User-Agent", text: $editedUserAgent, axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .lineLimit(2, reservesSpace: true)
                .font(.system(size: 11, design: .monospaced))
        }
    }

    // MARK: - Window size section

    private var windowSizeSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Window Size")
                .font(.headline)

            Text("Set the default browser window size in pixels.")
                .font(.caption)
                .foregroundColor(.secondary)

            HStack(spacing: 12) {
                VStack(alignment: .trailing, spacing: 4) {
                    Text("Width:")
                        .font(.caption)
                    TextField("Width", text: $editedWidth)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 90)
                        .font(.system(size: 12, design: .monospaced))
                }

                Text("×")
                    .font(.title3)
                    .foregroundColor(.secondary)
                    .padding(.top, 12)

                VStack(alignment: .trailing, spacing: 4) {
                    Text("Height:")
                        .font(.caption)
                    TextField("Height", text: $editedHeight)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 90)
                        .font(.system(size: 12, design: .monospaced))
                }
            }

            HStack(spacing: 8) {
                Button("Reset to 1200×800") {
                    editedWidth = "1200"
                    editedHeight = "800"
                    applyAll()
                }

                Spacer()

                if saved {
                    Text("Saved")
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .transition(.opacity)
                }

                Button("Apply") {
                    applyAll()
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
            }
        }
    }

    // MARK: - Apply

    private func applyAll() {
        // Apply window size
        if let w = Double(editedWidth), let h = Double(editedHeight),
           w >= 400, h >= 300, w <= 7680, h <= 4320 {
            viewModel.windowWidth = CGFloat(w)
            viewModel.windowHeight = CGFloat(h)
            viewModel.applyWindowSize()
        }

        // Apply user-agent
        let trimmed = editedUserAgent.trimmingCharacters(in: .whitespacesAndNewlines)
        customUserAgent = trimmed
        viewModel.setUserAgent(trimmed.isEmpty ? nil : trimmed)

        showSaved()
    }

    private func showSaved() {
        withAnimation { saved = true }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            withAnimation { saved = false }
        }
    }
}
