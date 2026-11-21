// Draws the Tango for Claude icons with CoreGraphics (no extra tools needed).
// Usage: swift draw.swift <outdir>
// Writes appicon.png (1024x1024), tray-template.png (44x44, black on clear,
// for macOS) and tray.png (44x44, white-on-coral, for Windows/Linux).
import AppKit

let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "."

func png(_ size: Int, _ draw: (CGContext, CGFloat) -> Void) -> Data {
    let cs = CGColorSpace(name: CGColorSpace.sRGB)!
    let ctx = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0,
                        space: cs, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    draw(ctx, CGFloat(size))
    let rep = NSBitmapImageRep(cgImage: ctx.makeImage()!)
    return rep.representation(using: .png, properties: [:])!
}

// Two people (seats) side by side, the back one slightly offset.
func people(_ c: CGContext, _ s: CGFloat, _ front: CGColor, _ back: CGColor, scale: CGFloat) {
    func person(_ cx: CGFloat, _ color: CGColor) {
        c.setFillColor(color)
        let head = s * 0.10 * scale
        c.fillEllipse(in: CGRect(x: cx - head, y: s * 0.54, width: head * 2, height: head * 2))
        let w = s * 0.32 * scale, h = s * 0.26 * scale
        let body = CGPath(roundedRect: CGRect(x: cx - w / 2, y: s * 0.24, width: w, height: h),
                          cornerWidth: w * 0.42, cornerHeight: h * 0.48, transform: nil)
        c.addPath(body); c.fillPath()
    }
    person(s * 0.60, back)
    person(s * 0.41, front)
}

let app = png(1024) { c, s in
    let inset = s * 0.09
    let rect = CGRect(x: inset, y: inset, width: s - 2 * inset, height: s - 2 * inset)
    let path = CGPath(roundedRect: rect, cornerWidth: s * 0.19, cornerHeight: s * 0.19, transform: nil)
    c.addPath(path); c.clip()
    let grad = CGGradient(colorsSpace: CGColorSpace(name: CGColorSpace.sRGB),
                          colors: [CGColor(red: 0.91, green: 0.52, blue: 0.36, alpha: 1),
                                   CGColor(red: 0.76, green: 0.25, blue: 0.05, alpha: 1)] as CFArray,
                          locations: [0, 1])!
    c.drawLinearGradient(grad, start: CGPoint(x: 0, y: s), end: CGPoint(x: s, y: 0), options: [])
    people(c, s, CGColor(red: 1, green: 1, blue: 1, alpha: 1), CGColor(red: 1, green: 0.86, blue: 0.78, alpha: 1), scale: 1)
}

let template = png(44) { c, s in
    people(c, s, CGColor(gray: 0, alpha: 1), CGColor(gray: 0, alpha: 0.55), scale: 1.15)
}

let tray = png(44) { c, s in
    c.setFillColor(CGColor(red: 0.85, green: 0.42, blue: 0.24, alpha: 1))
    c.fillEllipse(in: CGRect(x: 0, y: 0, width: s, height: s))
    people(c, s, CGColor(gray: 1, alpha: 1), CGColor(gray: 1, alpha: 0.7), scale: 1)
}

try! app.write(to: URL(fileURLWithPath: out + "/appicon.png"))
try! template.write(to: URL(fileURLWithPath: out + "/tray-template.png"))
try! tray.write(to: URL(fileURLWithPath: out + "/tray.png"))
print("ok")
