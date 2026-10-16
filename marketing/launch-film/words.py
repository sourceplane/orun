import json, glob, os, soundfile as sf
from faster_whisper import WhisperModel
m = WhisperModel("small.en", device="cpu", compute_type="int8")
out = {}
for f in sorted(glob.glob("voice/*.wav")):
    fid = os.path.basename(f)[:-4]
    segs, _ = m.transcribe(f, word_timestamps=True, language="en")
    words = [{"w": w.word.strip(), "start": round(w.start,2), "end": round(w.end,2)} for s in segs for w in s.words]
    d = sf.info(f).duration
    out[fid] = {"duration": round(d,2), "words": words}
    print(fid, d, " ".join(f"{w['w']}@{w['start']}" for w in words))
json.dump(out, open("audio_meta.json","w"), indent=1)
