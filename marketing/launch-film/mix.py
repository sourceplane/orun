"""Sound for the orun film: every sound is synthesised here (no third-party samples, no licences).
Cues resolve against the same measured word clock as the picture (timeline.json)."""
import json, re, subprocess, numpy as np, soundfile as sf

SR = 48000
T = json.load(open("timeline.json"))
N = int((T["total"] + 0.5) * SR)
vo = np.zeros(N); bed = np.zeros(N); fx = np.zeros(N)

def norm(s): return re.sub(r"[^a-z0-9]", "", s.lower())
def V(i): s = T["scenes"][i]; return s["start"] + s["lead"]
def S(i): return T["scenes"][i]["start"]
def E(i): s = T["scenes"][i]; return s["start"] + s["dur"]
def W(i, word, n=0):
    k = 0
    for w in T["meta"][i]["words"]:
        if norm(w["w"]) == norm(word):
            if k == n: return V(i) + w["start"]
            k += 1
    raise KeyError(word)

def put(buf, sig, t, gain=1.0):
    a = int(round(t * SR)); b = min(N, a + len(sig))
    if a < 0 or a >= N: return
    buf[a:b] += sig[: b - a] * gain

def tt(d): return np.arange(int(d * SR)) / SR
def env(d, a=0.005, r=None):
    t = tt(d); e = np.minimum(1, t / a) if a > 0 else np.ones_like(t)
    return e * (np.exp(-t / r) if r else 1)
rng = np.random.default_rng(7)  # fixed seed: the mix is reproducible
def noise(d): return rng.standard_normal(len(tt(d)))
def lp(x, fc):  # one-pole low-pass
    a = np.exp(-2 * np.pi * fc / SR); y = np.zeros_like(x); p = 0.0
    for i, v in enumerate(x): p = (1 - a) * v + a * p; y[i] = p
    return y
def hp(x, fc): return x - lp(x, fc)

# ---------- voice ----------
for i in T["scenes"]:
    wav = f"voice/{i}.wav"
    raw = subprocess.run(["node_modules/ffmpeg-static/ffmpeg", "-v", "quiet", "-i", wav, "-ar", str(SR), "-ac", "1", "-f", "f32le", "-"], capture_output=True).stdout
    put(vo, np.frombuffer(raw, dtype=np.float32).astype(float), V(i))

# ---------- bed: a slow D-minor(add9) pad, filtered, with a swell into the hero and a lift at the end ----------
t = np.arange(N) / SR
freqs = [73.42, 110.0, 146.83, 174.61, 220.0, 329.63]           # D2 A2 D3 F3 A3 E4
pad = np.zeros(N)
for k, f in enumerate(freqs):
    for det in (-0.12, 0.12):
        pad += np.sin(2 * np.pi * (f + det) * t + k) * (0.55 if f < 150 else 0.3)
pad += 0.12 * np.sin(2 * np.pi * 440.0 * t) * (0.5 + 0.5 * np.sin(2 * np.pi * 0.07 * t))
# final lift: the chord moves to F major (F2 C3 F3 A3 C4) under the sunrise
lift = np.zeros(N); lt = S("09-end")
for f in [87.31, 130.81, 174.61, 220.0, 261.63]:
    lift += np.sin(2 * np.pi * f * t) * (0.5 if f < 150 else 0.3)
xf = np.clip((t - lt) / 1.2, 0, 1)
pad = pad * (1 - xf) + lift * xf
pad = lp(pad, 900) / 3.2
shape = np.interp(t,
    [0, 1.0, S("06-baseline"), E("06-baseline") - 0.4, E("06-baseline") + 0.2, W("07-live", "later"), S("08-converge"), S("09-end"), T["total"] - 2.2, T["total"]],
    [0, 0.55, 0.62, 0.75, 0.0, 0.85, 0.7, 0.9, 0.9, 0.0])   # the bed drops out for the silence before the hero
bed = pad * shape

# ---------- sound events ----------
def shimmer(d=1.0):
    x = tt(d); f = 500 + 700 * x / d
    return np.sin(2 * np.pi * np.cumsum(f) / SR) * env(d, 0.3, 0.5) * 0.5 + hp(noise(d), 4000) * env(d, 0.4, 0.3) * 0.08
def key():   # a key tap: short band of noise with a body thump
    d = 0.05; return hp(noise(d), 2500) * env(d, 0.001, 0.008) * 0.8 + np.sin(2 * np.pi * 180 * tt(d)) * env(d, 0.001, 0.01) * 0.4
def pop(f=900):
    d = 0.12; return np.sin(2 * np.pi * f * tt(d)) * env(d, 0.002, 0.03)
def whoosh(d, up=True, lo=300, hi=3500):
    x = noise(d); p = tt(d) / d; out = np.zeros_like(x); seg = int(0.02 * SR)
    for a in range(0, len(x), seg):
        fc = lo + (hi - lo) * (p[a] if up else 1 - p[a]); out[a:a + seg] = x[a:a + seg]
    out = lp(hp(out, 200), 2600) * np.sin(np.pi * p) ** 2
    return out * 0.9
def sub(d=1.8, f=44.0):
    x = tt(d); return np.sin(2 * np.pi * f * x * (1 - 0.08 * x / d)) * env(d, 0.01, 0.7)
def thud():
    d = 0.35; return np.sin(2 * np.pi * 62 * tt(d)) * env(d, 0.002, 0.09) + lp(noise(d), 400) * env(d, 0.001, 0.03) * 0.6
def buzz():
    d = 0.32; x = tt(d); saw = 2 * ((110 * x) % 1) - 1
    return lp(saw, 1400) * env(d, 0.004, 0.12) * 0.7
def bell(f, d=1.6):
    x = tt(d); return (np.sin(2 * np.pi * f * x) + 0.4 * np.sin(2 * np.pi * f * 2.76 * x) + 0.2 * np.sin(2 * np.pi * f * 5.4 * x)) * env(d, 0.002, 0.45) * 0.4
def tick(f=2400):
    d = 0.03; return np.sin(2 * np.pi * f * tt(d)) * env(d, 0.001, 0.006)

# 01 — the line draws in silence; the myth is typed; struck
put(fx, shimmer(1.0), 0.12, 0.30)
b = W("01-hook", "because") - 0.02; e = W("01-hook", "faster", 1) + 0.3
myth = "because they type faster"
for j, ch in enumerate(myth):
    if ch != " ": put(fx, key(), b + (e - b) * j / len(myth), 0.09 + 0.025 * ((j * 7) % 3))
put(fx, lp(hp(noise(0.3), 800), 3000) * env(0.3, 0.02, 0.1), W("01-hook", "faster", 1) + 0.42, 0.22)   # strike
# 02 — the disciplines arrive (one stagger, three pops), then dim
for j, k in enumerate((0, 5, 10)):
    put(fx, pop(700 + 120 * j), W("02-platform", "years") - 0.04 + k * 0.045, 0.16)
put(fx, whoosh(0.6, False), W("02-platform", "never") - 0.1, 0.10)
# 03 — the intent card lands
put(fx, thud(), W("03-intent", "intent") - 0.05, 0.25)
# 04 — the compile sweep; the digests agree
SWEEP_AT = W("04-compile", "compiles") - 0.08
put(fx, whoosh(1.15, True), SWEEP_AT, 0.26)
put(fx, shimmer(1.15), SWEEP_AT, 0.10)
put(fx, bell(987.77, 1.2), W("04-compile", "every"), 0.20)
# 05 — the rule hits the line
HIT = W("05-policy", "rule") - 0.02
put(fx, whoosh(0.35, True), W("05-policy", "breaks"), 0.18)
put(fx, thud(), HIT, 0.32)
put(fx, buzz(), HIT + 0.02, 0.22)
# 06 — select, connect
put(fx, tick(1800), W("06-baseline", "baseline") + 0.12, 0.35)
put(fx, tick(2600), W("06-baseline", "connect") - 0.04, 0.22)
put(fx, tick(2600), W("06-baseline", "connect") + 0.12, 0.22)
# 07 — silence, then the hero: sub + sweep
UP_AT = W("07-live", "later") - 0.1
put(fx, sub(1.9), UP_AT, 0.30)
put(fx, whoosh(1.5, True, 200, 2500), UP_AT, 0.22)
# 08 — commit drops into the line; 41 jobs roll; all green
put(fx, whoosh(0.35, False), W("08-converge", "converges") - 0.1, 0.22)
F0 = W("08-converge", "converges") + 0.15; F1 = W("08-converge", "41") + 0.3
for j in range(41):
    put(fx, tick(2200 + 40 * (j % 5)), F0 + (F1 - F0) * j / 40, 0.10)
G = W("08-converge", "green")
for f, dt in ((523.25, 0), (659.25, 0.06), (783.99, 0.12)):
    put(fx, bell(f, 1.8), G - 0.05 + dt, 0.20)
# 09 — sunrise
put(fx, sub(2.2, 41.0), W("09-end", "o") - 0.1, 0.30)
put(fx, shimmer(1.4), W("09-end", "o") - 0.1, 0.18)

mix = vo * 1.4 + bed * 0.16 + fx * 0.9
sf.write("mix-raw.wav", mix.astype(np.float32), SR, subtype="FLOAT")
sf.write("stem-vo.wav", vo.astype(np.float32), SR, subtype="FLOAT")
print("peak", 20 * np.log10(np.max(np.abs(mix)) + 1e-9), "dBFS  total", T["total"])

# ---------- audit: is each cue audible against the voice in its own window? ----------
def db(x): return 20 * np.log10(max(x, 1e-9))
CUES = {"line draw": (0.12, 1.0), "typing": (b, e - b), "chips": (W("02-platform", "years") - 0.04, 0.5),
        "compile sweep": (SWEEP_AT, 1.15), "impact": (HIT, 0.35), "select": (W("06-baseline", "baseline") + 0.12, 0.05),
        "hero sub": (UP_AT, 1.5), "job roll": (F0, F1 - F0), "all green": (G - 0.05, 0.6), "sunrise": (W("09-end", "o") - 0.1, 1.4)}
for k, (t0, d) in CUES.items():
    a, z = int(t0 * SR), int((t0 + d) * SR)
    f = fx[a:z] * 0.9; v = vo[a:z] * 1.4
    print(f"{k:14s} fx peak {db(np.max(np.abs(f))):6.1f} dB   voice rms {db(np.sqrt(np.mean(v**2))):6.1f} dB   fx rms {db(np.sqrt(np.mean(f**2))):6.1f}")
