import SwiftUI

// MARK: - SettingsView

public struct SettingsView: View {
    @ObservedObject public var viewModel: WebViewModel

    public init(viewModel: WebViewModel) {
        self.viewModel = viewModel
    }

    @AppStorage("user_agent") private var customUserAgent: String = ""
    @State private var editedUserAgent: String = ""
    @State private var saved = false
    @State private var loadingUA = true

    public var body: some View {
        TabView {
            browserSettings
                .tabItem {
                    Label("Browser", systemImage: "globe")
                }
        }
        .frame(width: 480, height: 200)
        .task {
            if customUserAgent.isEmpty {
                editedUserAgent = await viewModel.currentUserAgent()
            } else {
                editedUserAgent = customUserAgent
            }
            loadingUA = false
        }
    }

    // MARK: - Browser settings

    private var browserSettings: some View {
        Form {
            userAgentSection
            applyButton
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

            TextField(loadingUA ? "Loading..." : "Custom User-Agent", text: $editedUserAgent, axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .lineLimit(2, reservesSpace: true)
                .font(.system(size: 11, design: .monospaced))
        }
    }

    // MARK: - Apply

    private var applyButton: some View {
        HStack {
            if saved {
                Text("Saved")
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .transition(.opacity)
            }

            Button("Apply") {
                let trimmed = editedUserAgent.trimmingCharacters(in: .whitespacesAndNewlines)
                customUserAgent = trimmed
                viewModel.setUserAgent(trimmed.isEmpty ? nil : trimmed)
                showSaved()
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.defaultAction)
        }
    }

    private func showSaved() {
        withAnimation { saved = true }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            withAnimation { saved = false }
        }
    }
}
