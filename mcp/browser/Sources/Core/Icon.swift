import AppKit

/// Generate a simple browser app icon programmatically.
public func generateAppIcon() -> NSImage {
    let size = CGSize(width: 256, height: 256)
    let image = NSImage(size: size)
    image.lockFocus()

    guard let ctx = NSGraphicsContext.current?.cgContext else {
        image.unlockFocus()
        return image
    }

    let rect = CGRect(origin: .zero, size: size)

    // Background: rounded rect with accent color
    let bgPath = NSBezierPath(roundedRect: rect, xRadius: 48, yRadius: 48)
    ctx.setFillColor(NSColor.controlAccentColor.cgColor)
    bgPath.fill()

    // Globe circle
    let globeRect = CGRect(x: 28, y: 28, width: 200, height: 200)
    ctx.setFillColor(NSColor.white.withAlphaComponent(0.95).cgColor)
    ctx.fillEllipse(in: globeRect)

    // "W" letter in center
    let text = "W" as NSString
    let font = NSFont.systemFont(ofSize: 96, weight: .bold)
    let textAttrs: [NSAttributedString.Key: Any] = [
        .font: font,
        .foregroundColor: NSColor.controlAccentColor,
    ]
    let textSize = text.size(withAttributes: textAttrs)
    let textRect = CGRect(
        x: (size.width - textSize.width) / 2,
        y: (size.height - textSize.height) / 2 - 8,
        width: textSize.width,
        height: textSize.height
    )
    text.draw(in: textRect, withAttributes: textAttrs)

    image.unlockFocus()
    return image
}

extension NSColor {
    static let browserIconBackground = NSColor(calibratedRed: 0.2, green: 0.4, blue: 0.9, alpha: 1.0)
}
