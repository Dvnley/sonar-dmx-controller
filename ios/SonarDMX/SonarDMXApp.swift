// Sonar DMX for iPad: runs the same controller as the Mac app (bound from Go
// as Sonar.xcframework) and shows its web UI full screen. The iPad sends the
// Art-Net itself, so no Mac is needed.
import Sonar
import SwiftUI
import WebKit

@main
struct SonarDMXApp: App {
    @Environment(\.scenePhase) private var phase
    @StateObject private var engine = Engine()

    var body: some Scene {
        WindowGroup {
            ZStack {
                Color(red: 14 / 255, green: 16 / 255, blue: 20 / 255).ignoresSafeArea()
                if let url = engine.url {
                    WebView(url: url, generation: engine.generation)
                } else if let error = engine.error {
                    VStack(spacing: 12) {
                        Text("Sonar DMX could not start").font(.title2).bold()
                        Text(error).foregroundStyle(.secondary).multilineTextAlignment(.center)
                        Button("Try again") { engine.start() }.buttonStyle(.borderedProminent)
                    }
                    .padding()
                } else {
                    ProgressView()
                }
            }
            .preferredColorScheme(.dark)
            .persistentSystemOverlays(.hidden)
            .onAppear {
                // A show controller should not dim or lock mid-show.
                UIApplication.shared.isIdleTimerDisabled = true
                engine.start()
            }
        }
        .onChange(of: phase) { phase in
            switch phase {
            case .active: engine.start()
            case .background: engine.save()
            default: break
            }
        }
    }
}

final class Engine: ObservableObject {
    @Published var url: URL?
    @Published var error: String?
    /// Bumped when the controller restarts, so the web view reloads.
    @Published var generation = 0

    func start() {
        let fm = FileManager.default
        let dir = fm.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("SonarDMX", isDirectory: true)
        try? fm.createDirectory(at: dir, withIntermediateDirectories: true)

        var err: NSError?
        let started: String? = SonarStart(dir.path, &err)
        if let err {
            error = err.localizedDescription
            return
        }
        guard let started, let next = URL(string: started) else {
            error = "The controller did not return an address."
            return
        }
        error = nil
        if url != next {
            url = next
            generation += 1
        }
    }

    func save() {
        _ = SonarSave(nil)
    }
}

struct WebView: UIViewRepresentable {
    let url: URL
    let generation: Int

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        // The UI checks for this to show iPad wording instead of Mac wording.
        config.applicationNameForUserAgent = "SonarDMXiPad"
        config.allowsInlineMediaPlayback = true
        config.mediaTypesRequiringUserActionForPlayback = []
        let view = WKWebView(frame: .zero, configuration: config)
        view.isOpaque = false
        view.backgroundColor = UIColor(red: 14 / 255, green: 16 / 255, blue: 20 / 255, alpha: 1)
        view.scrollView.backgroundColor = view.backgroundColor
        view.scrollView.contentInsetAdjustmentBehavior = .never
        view.navigationDelegate = context.coordinator
        view.uiDelegate = context.coordinator
        context.coordinator.load(url, generation: generation, in: view)
        return view
    }

    func updateUIView(_ view: WKWebView, context: Context) {
        context.coordinator.load(url, generation: generation, in: view)
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
        private var loaded = -1

        func load(_ url: URL, generation: Int, in view: WKWebView) {
            guard generation != loaded else { return }
            loaded = generation
            view.load(URLRequest(url: url))
        }

        // iOS can kill the page while the app is in the background.
        func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
            webView.reload()
        }

        // The controller starts a moment after launch; retry until it answers.
        func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { webView.reload() }
        }

        // Music mode's "Listen with this iPad's mic". iOS still asks once.
        func webView(_ webView: WKWebView, requestMediaCapturePermissionFor origin: WKSecurityOrigin,
                     initiatedByFrame frame: WKFrameInfo, type: WKMediaCaptureType,
                     decisionHandler: @escaping (WKPermissionDecision) -> Void) {
            decisionHandler(origin.host == "127.0.0.1" ? .grant : .prompt)
        }
    }
}
