#!/usr/bin/env python3
"""Record the README demo as an asciicast v2 file.

Drives a real bash in a pseudo-terminal: every command is typed, run, and
captured with its real output and colours. Nothing is edited. Two things are
presentation only: the typing rhythm and pauses, and the playback speed of a
command marked `slow` (the local run finishes in about a second, too fast to
watch, so its frames are played back at a fraction of real speed).

    scripts/demo/setup.sh /tmp/orun-demo/code
    go build -o /tmp/orun ./cmd/orun
    scripts/demo/record.py --workspace /tmp/orun-demo/code --orun /tmp/orun --out demo.cast
    npx svg-term-cli --in demo.cast --out assets/orun-demo.svg --window --no-cursor \\
        --width 96 --height 30 --padding 18 --term iterm2 --profile scripts/demo/orun.itermcolors
"""
import argparse
import fcntl
import json
import os
import pty
import random
import re
import select
import shutil
import struct
import sys
import tempfile
import termios
import time

COLS, ROWS = 96, 30
PROMPT_MARK = "❯"  # ❯

# Terminal capability queries some libraries send at start-up. A real
# terminal answers them; so does this one, and they are kept out of the cast.
QUERIES = {
    b"\x1b]11;?\x1b\\": b"\x1b]11;rgb:1414/1212/1e1e\x1b\\",
    b"\x1b]11;?\x07": b"\x1b]11;rgb:1414/1212/1e1e\x07",
    b"\x1b]10;?\x1b\\": b"\x1b]10;rgb:e4e4/e1e1/f0f0\x1b\\",
    b"\x1b[6n": b"\x1b[1;1R",
}


def plan_id(output):
    m = re.findall(r"orun run ([0-9a-f]{12})", output)
    return m[-1] if m else "latest"


# (command, seconds to linger afterwards, options). A command may be a function
# of the previous command's output.
SCENES = [
    ("orun new --blueprint saas-baseline/blueprint.yaml --out acme-shop --set name=acme-shop", 3.2, {}),
    ("cd acme-shop", 0.3, {}),
    ("ls", 1.4, {}),
    ("orun plan", 4.2, {}),
    (lambda prev: f"orun run {plan_id(prev)}", 4.5, {"slow": 3.0}),
]


class Recorder:
    def __init__(self, cwd, path_dir, home):
        self.events = []
        self.clock = 0.0  # playback time: output time plus scripted pauses
        self.speed = 1.0
        env = {
            "PATH": f"{path_dir}:/usr/local/bin:/usr/bin:/bin",
            "HOME": home,
            "TERM": "xterm-256color",
            "COLORTERM": "truecolor",
            "LANG": "C.UTF-8",
            "COLUMNS": str(COLS),
            "LINES": str(ROWS),
            "ORUN_NO_TUI": "1",
            "GIT_PAGER": "cat",
            "GIT_AUTHOR_NAME": "Acme", "GIT_AUTHOR_EMAIL": "eng@acme.dev",
            "GIT_COMMITTER_NAME": "Acme", "GIT_COMMITTER_EMAIL": "eng@acme.dev",
            "PS1": "\\[\\e[2m\\]\\W\\[\\e[0m\\] \\[\\e[38;5;141m\\]" + PROMPT_MARK + "\\[\\e[0m\\] ",
        }
        pid, fd = pty.fork()
        if pid == 0:
            os.chdir(cwd)
            os.execve("/bin/bash", ["bash", "--noprofile", "--norc", "-i"], env)
        self.pid, self.fd = pid, fd
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.last_real = time.monotonic()
        self.read_until_prompt()

    def _read(self):
        data = os.read(self.fd, 65536)
        for q, answer in QUERIES.items():
            if q in data:
                os.write(self.fd, answer)
                data = data.replace(q, b"")
        return data.decode("utf-8", "replace")

    def _emit(self, data):
        if not data:
            return
        now = time.monotonic()
        self.clock += min(now - self.last_real, 1.0) * self.speed
        self.last_real = now
        self.events.append([round(self.clock, 3), "o", data])

    def pause(self, seconds):
        self.clock += seconds
        self.last_real = time.monotonic()

    def read_until_prompt(self, record=True, timeout=60):
        buf = ""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            r, _, _ = select.select([self.fd], [], [], 0.1)
            if not r:
                if buf.rstrip().endswith(PROMPT_MARK) or buf.rstrip().endswith(PROMPT_MARK + "\x1b[0m"):
                    return buf
                continue
            chunk = self._read()
            buf += chunk
            if record:
                self._emit(chunk)
            else:
                self.last_real = time.monotonic()
        raise TimeoutError("prompt did not return; last output: " + repr(buf[-400:]))

    def run(self, text, rng, slow=None):
        self.pause(0.5)
        for i, ch in enumerate(text):
            os.write(self.fd, ch.encode())
            time.sleep(0.004)
            r, _, _ = select.select([self.fd], [], [], 0.2)
            if r:
                self._emit(self._read())
            gap = rng.uniform(0.03, 0.075)
            if ch == " " and rng.random() < 0.25:
                gap += rng.uniform(0.05, 0.15)
            self.pause(gap)
        self.pause(0.45)
        if slow:
            self.speed = slow
        os.write(self.fd, b"\r")
        out = self.read_until_prompt()
        self.speed = 1.0
        return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--workspace", required=True, help="directory built by setup.sh")
    ap.add_argument("--orun", required=True, help="orun binary to put on PATH")
    ap.add_argument("--out", required=True, help="asciicast file to write")
    args = ap.parse_args()

    bindir = tempfile.mkdtemp(prefix="orun-demo-bin-")
    shutil.copy(os.path.abspath(args.orun), os.path.join(bindir, "orun"))
    # `ls` with colours, as most shells alias it.
    with open(os.path.join(bindir, "ls"), "w") as f:
        f.write('#!/bin/sh\nexec /bin/ls --color=auto "$@"\n')
    os.chmod(os.path.join(bindir, "ls"), 0o755)
    home = tempfile.mkdtemp(prefix="orun-demo-home-")
    rec = Recorder(args.workspace, bindir, home)
    rng = random.Random(11)
    prev = ""
    for cmd, linger, opts in SCENES:
        text = cmd(prev) if callable(cmd) else cmd
        prev = rec.run(text, rng, slow=opts.get("slow"))
        rec.pause(linger)
    # A no-op event holds the last frame for the final linger.
    rec.events.append([round(rec.clock, 3), "o", "\x1b[0m"])
    os.write(rec.fd, b"exit\r")

    header = {"version": 2, "width": COLS, "height": ROWS, "timestamp": 0,
              "title": "orun: platform discipline as code",
              "env": {"TERM": "xterm-256color", "SHELL": "/bin/bash"}}
    with open(args.out, "w") as f:
        f.write(json.dumps(header) + "\n")
        for ev in rec.events:
            f.write(json.dumps(ev, ensure_ascii=False) + "\n")
    print(f"wrote {args.out}: {len(rec.events)} events, {rec.clock:.1f}s", file=sys.stderr)


if __name__ == "__main__":
    main()
