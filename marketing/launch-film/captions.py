import json, re
T=json.load(open('timeline.json')); S=json.load(open('script.json'))
def ts(t):
    ms=int(round(t*1000)); h,ms=divmod(ms,3600000); m,ms=divmod(ms,60000); s,ms=divmod(ms,1000)
    return f"{h:02d}:{m:02d}:{s:02d},{ms:03d}"
def ntok(p):  # count tokens the way whisper split them
    p=re.sub('(?i)forty-four','44',p); p=re.sub('(?i)forty-one','41',p)
    return len([x for x in re.split(r"[\s-]+", p) if x])
out=[]; n=1
for line in S:
    i=line['id']; sc=T['scenes'][i]; v=sc['start']+sc['lead']; ws=T['meta'][i]['words']
    parts=[p.strip() for p in line['say'].replace('?','?|').replace('. ','.|').replace(': ',':|').split('|') if p.strip()]
    wi=0
    for p in parts:
        k=ntok(p); seg=ws[wi:wi+k]; wi+=k
        assert seg, (i,p)
        out.append(f"{n}\n{ts(v+seg[0]['start'])} --> {ts(v+seg[-1]['end']+0.2)}\n{p.replace('Oh-run','orun')}\n"); n+=1
    assert wi==len(ws), (i, wi, len(ws))
open('orun-launch.en.srt','w').write("\n".join(out)); print(len(out),"cues ok")
