// Generates media/icon.png (256×256) and media/icon.svg without any dependency.
// Design: Argalla navy tile, a turquoise crescent (the night shift) and a blue
// check mark (reviewed in the morning). Rendered with signed-distance functions
// at 4× and box-downsampled, same technique as the other Argalla extensions.
import fs from 'node:fs';
import path from 'node:path';
import zlib from 'node:zlib';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const OUT_PNG = path.join(ROOT, 'media', 'icon.png');
const OUT_SVG = path.join(ROOT, 'media', 'icon.svg');
const OUT_ACT = path.join(ROOT, 'media', 'activity.svg');

const SIZE = 256;
const UNIT = 128;
const K = SIZE / UNIT;
const SS = 4;
const NAVY = [0x0f, 0x17, 0x2a];
const TURQ = [0x2d, 0xd4, 0xbf];
const BLUE = [0x3b, 0x82, 0xf6];

const RADIUS = 28;
// crescent: big circle minus a shifted circle
const MOON = { cx: 52, cy: 52, r: 26, bite: { cx: 64, cy: 44, r: 22 } };
// check mark
const STROKE = 12;
const CHECK = { a: [62, 88], b: [78, 104], c: [108, 70] };

const sdRoundRect = (x, y, w, h, r) => {
  const qx = Math.abs(x - w / 2) - (w / 2 - r);
  const qy = Math.abs(y - h / 2) - (h / 2 - r);
  return Math.min(Math.max(qx, qy), 0) + Math.hypot(Math.max(qx, 0), Math.max(qy, 0)) - r;
};
const sdCircle = (x, y, cx, cy, r) => Math.hypot(x - cx, y - cy) - r;
const sdSegment = (px, py, [ax, ay], [bx, by]) => {
  const vx = bx - ax, vy = by - ay, wx = px - ax, wy = py - ay;
  const t = Math.max(0, Math.min(1, (wx * vx + wy * vy) / (vx * vx + vy * vy)));
  return Math.hypot(wx - t * vx, wy - t * vy);
};

const W = SIZE * SS;
const rgba = new Uint8ClampedArray(W * W * 4);
for (let j = 0; j < W; j++) {
  for (let i = 0; i < W; i++) {
    const x = (i + 0.5) / SS / K, y = (j + 0.5) / SS / K;
    let r = 0, g = 0, b = 0, a = 0;
    if (sdRoundRect(x, y, UNIT, UNIT, RADIUS) <= 0) {
      [r, g, b] = NAVY; a = 255;
      const inMoon = sdCircle(x, y, MOON.cx, MOON.cy, MOON.r) <= 0 && sdCircle(x, y, MOON.bite.cx, MOON.bite.cy, MOON.bite.r) > 0;
      if (inMoon) [r, g, b] = TURQ;
      const dCheck = Math.min(sdSegment(x, y, CHECK.a, CHECK.b), sdSegment(x, y, CHECK.b, CHECK.c));
      if (dCheck <= STROKE / 2) [r, g, b] = BLUE;
    }
    const o = (j * W + i) * 4;
    rgba[o] = r; rgba[o + 1] = g; rgba[o + 2] = b; rgba[o + 3] = a;
  }
}
const px = new Uint8Array(SIZE * SIZE * 4);
for (let j = 0; j < SIZE; j++) {
  for (let i = 0; i < SIZE; i++) {
    let r = 0, g = 0, b = 0, a = 0;
    for (let sj = 0; sj < SS; sj++) for (let si = 0; si < SS; si++) {
      const o = ((j * SS + sj) * W + (i * SS + si)) * 4;
      const al = rgba[o + 3] / 255;
      r += rgba[o] * al; g += rgba[o + 1] * al; b += rgba[o + 2] * al; a += al;
    }
    const n = SS * SS;
    const o = (j * SIZE + i) * 4;
    if (a > 0) { px[o] = r / a; px[o + 1] = g / a; px[o + 2] = b / a; }
    px[o + 3] = Math.round((a / n) * 255);
  }
}

// --- PNG encoder (no deps) ---
const crcTable = new Int32Array(256);
for (let n = 0; n < 256; n++) { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; crcTable[n] = c; }
const crc32 = (buf) => { let c = -1; for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8); return (c ^ -1) >>> 0; };
const chunk = (type, data) => {
  const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
};
const ihdr = Buffer.alloc(13);
ihdr.writeUInt32BE(SIZE, 0); ihdr.writeUInt32BE(SIZE, 4); ihdr[8] = 8; ihdr[9] = 6; ihdr[10] = 0; ihdr[11] = 0; ihdr[12] = 0;
const raw = Buffer.alloc((SIZE * 4 + 1) * SIZE);
for (let j = 0; j < SIZE; j++) { raw[j * (SIZE * 4 + 1)] = 0; Buffer.from(px.buffer, j * SIZE * 4, SIZE * 4).copy(raw, j * (SIZE * 4 + 1) + 1); }
const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  chunk('IHDR', ihdr), chunk('IDAT', zlib.deflateSync(raw, { level: 9 })), chunk('IEND', Buffer.alloc(0)),
]);
fs.mkdirSync(path.dirname(OUT_PNG), { recursive: true });
fs.writeFileSync(OUT_PNG, png);

// --- SVG twin (for docs) ---
const hex = ([r, g, b]) => `#${[r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('')}`;
fs.writeFileSync(OUT_SVG, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" width="128" height="128">
  <rect width="128" height="128" rx="${RADIUS}" fill="${hex(NAVY)}"/>
  <mask id="m"><rect width="128" height="128" fill="#fff"/><circle cx="${MOON.bite.cx}" cy="${MOON.bite.cy}" r="${MOON.bite.r}" fill="#000"/></mask>
  <circle cx="${MOON.cx}" cy="${MOON.cy}" r="${MOON.r}" fill="${hex(TURQ)}" mask="url(#m)"/>
  <polyline points="${CHECK.a} ${CHECK.b} ${CHECK.c}" fill="none" stroke="${hex(BLUE)}" stroke-width="${STROKE}" stroke-linecap="round" stroke-linejoin="round"/>
</svg>
`);

// --- Activity bar icon: monochrome, 24×24, currentColor (VS Code tints it) ---
fs.writeFileSync(OUT_ACT, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
  <path d="M13.5 3.2A7.4 7.4 0 1 0 20.8 10.5 5.6 5.6 0 0 1 13.5 3.2z"/>
  <path d="M12.2 15.6l2 2 3.6-4"/>
</svg>
`);
console.log('icon.png, icon.svg, activity.svg written');
