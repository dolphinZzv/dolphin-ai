#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

BIN_DIR="$(cd ../../bin && pwd)"

echo "Building BrowserMCP..."
swift build -c release

BINARY=".build/release/BrowserMCP"
cp "$BINARY" "$BIN_DIR/cdp-server"

# Create .app bundle
APP_BUNDLE="$BIN_DIR/BrowserMCP.app"
rm -rf "$APP_BUNDLE"
mkdir -p "$APP_BUNDLE/Contents/MacOS" "$APP_BUNDLE/Contents/Resources"

cp "$BINARY" "$APP_BUNDLE/Contents/MacOS/BrowserMCP"

# Generate icon using Python
python3 -c "
import struct, zlib, os, shutil

w, h = 256, 256
raw = bytearray()
for y in range(h):
    raw.append(0)
    for x in range(w):
        cx, cy = x - 128, y - 128
        inside = abs(cx) < 85 and abs(cy) < 85
        corner = (abs(cx) - 85)**2 + (abs(cy) - 85)**2
        if inside or (abs(cx) > 85 and abs(cy) > 85 and corner < 2500):
            raw.extend([51, 102, 230, 255])
        elif (cx*cx + cy*cy)**0.5 < 90:
            raw.extend([255, 255, 255, 255])
        else:
            raw.extend([0, 0, 0, 0])

def write_png(path, rw):
    sig = b'\x89PNG\r\n\x1a\n'
    ihdr_data = struct.pack('>IIBBBBB', rw, rw, 8, 6, 0, 0, 0)
    ihdr = struct.pack('>I', 13) + b'IHDR' + ihdr_data + struct.pack('>I', zlib.crc32(b'IHDR' + ihdr_data) & 0xffffffff)
    pixel = bytearray()
    for y in range(rw):
        pixel.append(0)
        for x in range(rw):
            cx, cy = x - rw//2, y - rw//2
            inside = abs(cx) < rw//3 and abs(cy) < rw//3
            corner = (abs(cx) - rw//3)**2 + (abs(cy) - rw//3)**2
            if inside or (abs(cx) > rw//3 and abs(cy) > rw//3 and corner < (rw//5)**2):
                pixel.extend([51, 102, 230, 255])
            elif (cx*cx + cy*cy)**0.5 < rw//2.8:
                pixel.extend([255, 255, 255, 255])
            else:
                pixel.extend([0, 0, 0, 0])
    compressed = zlib.compress(bytes(pixel))
    idat = struct.pack('>I', len(compressed)) + b'IDAT' + compressed + struct.pack('>I', zlib.crc32(b'IDAT' + compressed) & 0xffffffff)
    iend = struct.pack('>I', 0) + b'IEND' + struct.pack('>I', zlib.crc32(b'IEND') & 0xffffffff)
    with open(path, 'wb') as f:
        f.write(sig + ihdr + idat + iend)

os.makedirs('/tmp/browser-icon.iconset', exist_ok=True)
for s in [16, 32, 64, 128, 256, 512]:
    write_png(f'/tmp/browser-icon.iconset/icon_{s}x{s}.png', s)
for s in [16, 32, 64, 128, 256]:
    shutil.copy(f'/tmp/browser-icon.iconset/icon_{s}x{s}.png', f'/tmp/browser-icon.iconset/icon_{s*2}x{s*2}.png')
os.system(f'iconutil -c icns /tmp/browser-icon.iconset -o \"$APP_BUNDLE/Contents/Resources/AppIcon.icns\" 2>&1')
" 2>&1

# Create Info.plist
cat > "$APP_BUNDLE/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>BrowserMCP</string>
    <key>CFBundleIdentifier</key>
    <string>space.siciv.browser-mcp</string>
    <key>CFBundleName</key>
    <string>BrowserMCP</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
EOF

echo "Done: bin/cdp-server + bin/BrowserMCP.app"
