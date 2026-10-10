// Derives every scene's length from its measured voiceover, then writes index.html + timeline.json.
import { readFileSync, writeFileSync, mkdirSync, copyFileSync } from 'node:fs';
const meta = JSON.parse(readFileSync('project/audio_meta.json', 'utf8'));
// [id, lead (s before VO), hold (s after VO)]
const PLAN = [
  ['01-hook', 1.00, 0.55], ['02-platform', 0.15, 0.45], ['03-intent', 0.25, 0.40],
  ['04-compile', 0.20, 0.55], ['05-policy', 0.20, 0.65], ['06-baseline', 0.20, 0.40],
  ['07-live', 0.40, 0.70], ['08-converge', 0.20, 0.75], ['09-end', 0.55, 2.90],
];
let t = 0; const scenes = {};
for (const [id, lead, hold] of PLAN) {
  const vo = meta[id].duration;
  const dur = +(lead + vo + hold).toFixed(3);
  scenes[id] = { start: +t.toFixed(3), lead, vo, dur };
  t += dur;
}
const total = +t.toFixed(3);
const timeline = { total, scenes, meta };
writeFileSync('timeline.json', JSON.stringify(timeline, null, 1));
let html = readFileSync('src/index.template.html', 'utf8');
html = html.replace('/*__TIMELINE__*/null', JSON.stringify(timeline)).replaceAll('__TOTAL__', String(total));
mkdirSync('project/vendor', { recursive: true });
copyFileSync('node_modules/gsap/dist/gsap.min.js', 'project/vendor/gsap.min.js');
writeFileSync('project/index.html', html);
console.log('total', total); for (const [k, v] of Object.entries(scenes)) console.log(k, v.start, v.dur);
