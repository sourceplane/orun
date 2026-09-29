import json, soundfile as sf
from kokoro_onnx import Kokoro
k = Kokoro("models/kokoro-v1.0.onnx", "models/voices-v1.0.bin")
for line in json.load(open("script.json")):
    s, sr = k.create(line["say"], voice="af_heart", speed=1.0, lang="en-us")
    sf.write(f"voice/{line['id']}.wav", s, sr)
    print(line["id"], round(len(s)/sr,2))
