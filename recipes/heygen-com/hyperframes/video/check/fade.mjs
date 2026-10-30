// usage: node fade.mjs VIDEO
//
// The known sample (sample.html): white title text fading in over one second on a
// dark ground. Fails unless the text is absent at the start and present by 2.5 s.
import { execFileSync } from "node:child_process";

const video = process.argv[2];
const frame = (at) => execFileSync("ffmpeg", ["-v", "error", "-ss", String(at), "-i", video,
  "-frames:v", "1", "-vf", "scale=160:90,format=gray", "-f", "rawvideo", "-"]);
const bright = (pixels) => pixels.filter((v) => v > 128).length;

const early = bright(frame(0.05));
const late = bright(frame(2.5));
if (early > 20) throw new Error(`text already visible at 0.05 s (${early} bright pixels)`);
if (late < 200) throw new Error(`no title at 2.5 s (${late} bright pixels)`);
console.log(`OK fade-in: ${early} bright pixels at 0.05 s, ${late} at 2.5 s`);
