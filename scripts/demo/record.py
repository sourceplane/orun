#!/usr/bin/env python3
"""Record the README demo as an asciicast v2 file.

Drives a real bash in a pseudo-terminal: every command is typed, run, and
captured with its real output and colours. Only the pauses are scripted.

    scripts/demo/setup.sh /tmp/orun-demo
    scripts/demo/record.py --workspace /tmp/orun-demo --orun ./orun --out demo.cast
    npx svg-term-cli --in demo.cast --out assets/orun-demo.svg --window --no-cursor --width 120 --height 34

The commands below are the whole script; edit SCENES to change the demo.
"""
import argparse
import json
import os
import pty
import random
import select
import shutil
import subprocess
import sys
import tempfile
import time

COLS, ROWS = 120, 34
PROMPT_MARK = "▲"  # ▲, the orun wedge, ends the prompt

# Each scene is a list of (command, seconds to linger on the output afterwards).
# Lines starting with "#" are typed as narration and run as bash comments.
SCENES = [
    [
        ("# This is an orun-disciplined platform repo: structure and standards as code.", 0.6),
        ("# 1 · DECLARE — a component says what it is and which golden path it follows", 0.4),
        ("sed -n '1,25p' apps/identity-worker/component.yaml", 3.5),
        ("orun compositions    # the golden paths, owned by the platform team", 3.5),
    ],
    [
        ("clear", 0.2),
        ("# 2 · STANDARDS ARE ENFORCED — break the contract and the plan refuses it", 0.4),
        ("sed -i '/deployCommand/d' apps/identity-worker/component.yaml", 0.4),
        ("orun plan", 4.0),
        ("git checkout -q apps/identity-worker/component.yaml   # put it back", 0.4),
        ("orun validate && orun plan", 4.0),
    ],
    [
        ("clear", 0.2),
        ("# 3 · THE CATALOG — derived from intent and CODEOWNERS, never curated", 0.4),
        ("orun catalog list", 3.5),
        ("orun catalog tree", 3.0),
        ("echo '# tune the VPC' >> infra/infra-1/main.tf   # change one component", 0.4),
        ("orun catalog affected --base main    # what it touches, and what re-runs", 4.5),
    ],
    [
        ("clear", 0.2),
        ("# 4 · AGENTS read the same model, through one MCP server", 0.4),
        ("orun mcp tools | cut -c1-112 | head -12", 4.0),
    ],
    [
        ("clear", 0.2),
        ("# 5 · BASELINES — package the standards, rebuild them as a new product", 0.4),
        ("cd ..", 0.2),
        ("orun new --blueprint acme-baseline/blueprint.yaml --out acme-shop --set serviceName=checkout-api", 4.5),
        ("# the platform team ships acme-baseline v1.1.0 (golden path moves to setup-node v5)", 0.4),
        ("git -C acme-baseline checkout -q v1.1.0", 0.3),
        ("orun new upgrade --out acme-shop --blueprint acme-baseline/blueprint.yaml", 5.0),
    ],
]


class Recorder:
    def __init__(self, cwd, path_dir, home):
        self.events = []
        self.start = None
        self.clock = 0.0  # virtual time: real output time plus scripted pauses
        env = {
            "PATH": f"{path_dir}:/usr/local/bin:/usr/bin:/bin",
            "HOME": home,
            "TERM": "xterm-256color",
            "LANG": "C.UTF-8",
            "COLUMNS": str(COLS),
            "LINES": str(ROWS),
            "ORUN_NO_TUI": "1",
            "GIT_PAGER": "cat",
            "PS1": "\\[\\e[38;5;141m\\]" + PROMPT_MARK + "\\[\\e[0m\\] \\[\\e[2m\\]\\W\\[\\e[0m\\] $ ",
        }
        pid, fd = pty.fork()
        if pid == 0:
            os.chdir(cwd)
            os.execve("/bin/bash", ["bash", "--noprofile", "--norc", "-i"], env)
        self.pid, self.fd = pid, fd
        import fcntl, struct, termios
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.last_real = time.monotonic()
        self.read_until_prompt(record=False)

    def _emit(self, data):
        now = time.monotonic()
        # Real elapsed time, with long silences capped so slow commands stay watchable.
        self.clock += min(now - self.last_real, 1.2)
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
                if buf.rstrip().endswith("$") and PROMPT_MARK in buf[-200:]:
                    return
                continue
            chunk = os.read(self.fd, 65536).decode("utf-8", "replace")
            buf += chunk
            if record:
                self._emit(chunk)
            else:
                self.last_real = time.monotonic()
        raise TimeoutError("prompt did not return; last output: " + repr(buf[-400:]))

    def type_command(self, text, rng):
        for ch in text:
            os.write(self.fd, ch.encode())
            # Echo comes back through the pty; read it so it is timestamped as typed.
            time.sleep(0.004)
            r, _, _ = select.select([self.fd], [], [], 0.2)
            if r:
                self._emit(os.read(self.fd, 4096).decode("utf-8", "replace"))
            self.pause(rng.uniform(0.025, 0.06) if not text.startswith("#") else rng.uniform(0.018, 0.04))
        self.pause(0.35)
        os.write(self.fd, b"\r")
        self.read_until_prompt()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--workspace", required=True, help="directory built by setup.sh")
    ap.add_argument("--orun", required=True, help="orun binary to put on PATH")
    ap.add_argument("--out", required=True, help="asciicast file to write")
    args = ap.parse_args()

    bindir = tempfile.mkdtemp(prefix="orun-demo-bin-")
    shutil.copy(os.path.abspath(args.orun), os.path.join(bindir, "orun"))
    home = tempfile.mkdtemp(prefix="orun-demo-home-")
    rec = Recorder(os.path.join(args.workspace, "platform"), bindir, home)
    rng = random.Random(7)
    rec.pause(0.5)
    for scene in SCENES:
        for cmd, linger in scene:
            rec.type_command(cmd, rng)
            rec.pause(linger)
    rec.pause(1.5)
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
