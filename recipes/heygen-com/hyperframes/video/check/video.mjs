// usage: node video.mjs COMPOSITION VIDEO
//
// Fails unless the video is what the composition declares, for any composition:
// a video stream at the root's data-width x data-height, data-duration long, and
// a late frame that is not one flat colour.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

const [composition, video] = process.argv.slice(2);
const root = readFileSync(composition, "utf8").match(/<[^>]*\bdata-composition-id="[^"]*"[^>]*>/);
if (!root) throw new Error("composition has no data-composition-id root");
const attr = (name) => Number(root[0].match(new RegExp(`\\bdata-${name}="([\\d.]+)"`))?.[1]);
const want = { duration: attr("duration"), width: attr("width"), height: attr("height") };

const probe = JSON.parse(execFileSync("ffprobe", ["-v", "error", "-select_streams", "v:0",
  "-show_entries", "stream=codec_name,width,height:format=duration", "-of", "json", video]));
const stream = probe.streams?.[0];
if (!stream) throw new Error("no video stream");
const duration = Number(probe.format.duration);
if (stream.width !== want.width || stream.height !== want.height)
  throw new Error(`video is ${stream.width}x${stream.height}, composition declares ${want.width}x${want.height}`);
if (Math.abs(duration - want.duration) > 0.2)
  throw new Error(`video is ${duration} s, composition declares ${want.duration} s`);

const frame = execFileSync("ffmpeg", ["-v", "error", "-ss", String(duration * 0.9), "-i", video,
  "-frames:v", "1", "-vf", "scale=160:90,format=gray", "-f", "rawvideo", "-"]);
const levels = new Set(frame).size;
if (levels <= 2) throw new Error(`late frame is flat (${levels} grey levels)`);
console.log(`OK ${stream.codec_name} ${stream.width}x${stream.height} ${duration} s, late frame ${levels} grey levels`);
