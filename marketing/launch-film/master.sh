#!/usr/bin/env bash
# two-pass loudnorm -> true-peak limiter (no makeup gain) on the mix; writes master.wav
set -euo pipefail
FF=node_modules/ffmpeg-static/ffmpeg
M=$($FF -hide_banner -i mix-raw.wav -af alimiter=limit=0.33:level=disabled:attack=2:release=80,loudnorm=I=-14:TP=-1.2:LRA=9:print_format=json -f null - 2>&1 | sed -n '/^{/,/^}/p')
g(){ echo "$M" | python3 -c "import json,sys;print(json.load(sys.stdin)['$1'])"; }
AF="alimiter=limit=0.33:level=disabled:attack=2:release=80,loudnorm=I=-14:TP=-1.2:LRA=9:linear=true:measured_I=$(g input_i):measured_TP=$(g input_tp):measured_LRA=$(g input_lra):measured_thresh=$(g input_thresh):offset=$(g target_offset),alimiter=limit=0.74:level=disabled:attack=3:release=60"
$FF -y -hide_banner -v error -i mix-raw.wav -af "$AF" -ar 48000 master.wav
$FF -hide_banner -i master.wav -af ebur128=peak=true:framelog=quiet -f null - 2>&1 | grep -E "^\s+(I|Peak|LRA):"
