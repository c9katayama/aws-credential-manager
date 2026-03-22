import AppKit

guard CommandLine.arguments.count >= 2 else {
  fputs("usage: render-app-icon.swift <output-png-path>\n", stderr)
  exit(1)
}

let outputURL = URL(fileURLWithPath: CommandLine.arguments[1])
let canvasSize = NSSize(width: 1024, height: 1024)
func aspectFitRect(contentSize: NSSize, boundingRect: NSRect) -> NSRect {
  guard contentSize.width > 0, contentSize.height > 0 else { return boundingRect }
  let widthScale = boundingRect.width / contentSize.width
  let heightScale = boundingRect.height / contentSize.height
  let scale = min(widthScale, heightScale)
  let fittedSize = NSSize(width: contentSize.width * scale, height: contentSize.height * scale)
  return NSRect(
    x: boundingRect.midX - (fittedSize.width / 2),
    y: boundingRect.midY - (fittedSize.height / 2),
    width: fittedSize.width,
    height: fittedSize.height
  )
}
guard
  let bitmap = NSBitmapImageRep(
    bitmapDataPlanes: nil,
    pixelsWide: Int(canvasSize.width),
    pixelsHigh: Int(canvasSize.height),
    bitsPerSample: 8,
    samplesPerPixel: 4,
    hasAlpha: true,
    isPlanar: false,
    colorSpaceName: .deviceRGB,
    bytesPerRow: 0,
    bitsPerPixel: 0
  )
else {
  fputs("failed to create bitmap\n", stderr)
  exit(1)
}

guard let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
  fputs("failed to create graphics context\n", stderr)
  exit(1)
}

NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = context
defer { NSGraphicsContext.restoreGraphicsState() }

NSColor.clear.setFill()
NSBezierPath(rect: NSRect(origin: .zero, size: canvasSize)).fill()

let inset: CGFloat = 72
let backgroundRect = NSRect(
  x: inset,
  y: inset,
  width: canvasSize.width - (inset * 2),
  height: canvasSize.height - (inset * 2)
)

let background = NSBezierPath(roundedRect: backgroundRect, xRadius: 210, yRadius: 210)
NSColor(calibratedWhite: 0.94, alpha: 1.0).setFill()
background.fill()

let shadow = NSShadow()
shadow.shadowBlurRadius = 24
shadow.shadowOffset = NSSize(width: 0, height: -10)
shadow.shadowColor = NSColor(calibratedWhite: 0.0, alpha: 0.10)
shadow.set()
background.fill()

guard
  let baseSymbol = NSImage(systemSymbolName: "key.horizontal.fill", accessibilityDescription: nil),
  let symbol = baseSymbol.withSymbolConfiguration(
    NSImage.SymbolConfiguration(pointSize: 560, weight: .regular)
  )
else {
  fputs("failed to create key symbol\n", stderr)
  exit(1)
}

let symbolBoundingRect = NSRect(x: 132, y: 228, width: 760, height: 568)
let symbolRect = aspectFitRect(contentSize: symbol.size, boundingRect: symbolBoundingRect)
let tintedSymbol = NSImage(size: symbol.size)
tintedSymbol.lockFocus()
NSColor(calibratedRed: 0.12, green: 0.14, blue: 0.18, alpha: 1.0).set()
symbol.draw(in: NSRect(origin: .zero, size: symbol.size))
tintedSymbol.unlockFocus()
tintedSymbol.isTemplate = false
tintedSymbol.draw(in: symbolRect)

guard let pngData = bitmap.representation(using: .png, properties: [:]) else {
  fputs("failed to encode png\n", stderr)
  exit(1)
}

try pngData.write(to: outputURL)
