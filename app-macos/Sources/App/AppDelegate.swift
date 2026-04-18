import AppKit
import SwiftUI

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
  private let helperClient = HelperClient()
  private lazy var viewModel = AppViewModel(helperClient: helperClient)
  private var statusItem: NSStatusItem?
  private var window: NSWindow?

  func applicationDidFinishLaunching(_ notification: Notification) {
    NSApp.setActivationPolicy(.accessory)
    NSApp.mainMenu = makeMainMenu()

    do {
      try helperClient.start()
    } catch {
      viewModel.lastError = error.localizedDescription
      viewModel.helperStatus = "Helper error"
    }

    let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    configureStatusItem(statusItem)
    self.statusItem = statusItem

    window = makeWindow()
    viewModel.setOpenOnePasswordHandler { [weak self] in
      self?.openOnePassword()
    }
    viewModel.bootstrap()
  }

  func applicationWillTerminate(_ notification: Notification) {
    helperClient.stop()
  }

  func windowWillClose(_ notification: Notification) {
    NSApp.hide(nil)
  }

  @objc private func toggleWindow(_ sender: AnyObject?) {
    guard let window else { return }

    if window.isVisible, NSApp.isActive {
      window.orderOut(sender)
      return
    }

    showWindow()
  }

  private func showWindow() {
    guard let window else { return }
    NSApp.activate(ignoringOtherApps: true)
    window.makeKeyAndOrderFront(nil)
    window.center()
  }

  private func openOnePassword() {
    NSApp.activate(ignoringOtherApps: true)
    NSWorkspace.shared.launchApplication("1Password")
  }

  private func makeWindow() -> NSWindow {
    let contentView = ContentView(
      viewModel: viewModel,
      onQuit: {
        NSApp.terminate(nil)
      }
    )
    let hostingController = NSHostingController(rootView: contentView)

    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 760, height: 560),
      styleMask: [.titled, .closable, .miniaturizable, .resizable],
      backing: .buffered,
      defer: false
    )
    window.title = "AWS Credential Manager"
    window.isReleasedWhenClosed = false
    window.center()
    window.contentViewController = hostingController
    window.delegate = self
    window.setFrameAutosaveName("AwsCredentialManagerMainWindow")
    window.collectionBehavior = [.fullScreenAuxiliary]
    return window
  }

  private func configureStatusItem(_ statusItem: NSStatusItem) {
    guard let button = statusItem.button else { return }

    if let image = makeStatusItemImage() {
      button.image = image
      button.imagePosition = .imageOnly
    } else {
      statusItem.length = NSStatusItem.variableLength
      button.title = "AWS"
    }

    button.toolTip = "AWS Credential Manager"
    button.setAccessibilityLabel("AWS Credential Manager")
    button.target = self
    button.action = #selector(toggleWindow(_:))
  }

  private func makeStatusItemImage() -> NSImage? {
    guard let image = NSImage(systemSymbolName: "key.horizontal.fill", accessibilityDescription: "AWS Credential Manager") else {
      return nil
    }
    image.isTemplate = true
    image.size = NSSize(width: 18, height: 18)
    return image
  }

  private func makeMainMenu() -> NSMenu {
    let mainMenu = NSMenu()

    let appMenuItem = NSMenuItem()
    mainMenu.addItem(appMenuItem)
    let appMenu = NSMenu()
    appMenuItem.submenu = appMenu
    appMenu.addItem(
      withTitle: "Hide AWS Credential Manager",
      action: #selector(NSApplication.hide(_:)),
      keyEquivalent: "h"
    )
    appMenu.addItem(.separator())
    appMenu.addItem(
      withTitle: "Quit AWS Credential Manager",
      action: #selector(NSApplication.terminate(_:)),
      keyEquivalent: "q"
    )

    let editMenuItem = NSMenuItem()
    mainMenu.addItem(editMenuItem)
    let editMenu = NSMenu(title: "Edit")
    editMenuItem.submenu = editMenu
    editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
    editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "Z")
    editMenu.addItem(.separator())
    editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
    editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
    editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
    editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")

    return mainMenu
  }
}
