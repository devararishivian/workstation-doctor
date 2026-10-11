#!/usr/bin/env python3
"""
Automated demo driver for workstation-doctor.
Runs workstation-doctor inside a pseudo-terminal and feeds choreographed
keystrokes to showcase all features, outputting asciicast for agg generation.
"""

import fcntl
import json
import os
import pty
import re
import select
import struct
import sys
import termios
import time

COLS = 130
ROWS = 38

KEY_UP = b"\x1b[A"
KEY_DOWN = b"\x1b[B"
KEY_ENTER = b"\r"
KEY_SPACE = b" "
KEY_ESC = b"\x1b"
KEY_B = b"b"
KEY_F = b"f"
KEY_R = b"r"
KEY_H = b"h"
KEY_Y = b"y"
KEY_Q = b"q"


def reset_package_fixture():
    """Ensure opencode-ai has version 1.18.35 so an actual update is available."""
    pkg_path = "/opt/homebrew/lib/node_modules/opencode-ai/package.json"
    if os.path.exists(pkg_path):
        try:
            with open(pkg_path) as f:
                d = json.load(f)
            d["version"] = "1.18.35"
            with open(pkg_path, "w") as f:
                json.dump(d, f, indent=2)
        except Exception as e:
            sys.stderr.write(f"Warning: could not reset package fixture: {e}\n")


def main():
    reset_package_fixture()

    pid, master = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.execvp("./workstation-doctor", ["./workstation-doctor"])
        sys.exit(1)

    winsize = struct.pack("HHHH", ROWS, COLS, 0, 0)
    fcntl.ioctl(master, termios.TIOCSWINSZ, winsize)

    raw_buf = b""

    def pump(duration=0.5):
        nonlocal raw_buf
        end = time.time() + duration
        while time.time() < end:
            timeout = max(0.01, min(0.05, end - time.time()))
            r, _, _ = select.select([master], [], [], timeout)
            if r:
                try:
                    data = os.read(master, 4096)
                    if not data:
                        break
                    sys.stdout.buffer.write(data)
                    sys.stdout.buffer.flush()
                    raw_buf += data
                    if len(raw_buf) > 50000:
                        raw_buf = raw_buf[-25000:]
                except OSError:
                    break

    def wait_for_pattern(pattern, timeout=12.0):
        start = time.time()
        while time.time() - start < timeout:
            pump(0.1)
            if pattern in raw_buf:
                return True
        return False

    def send_key(key, delay_after=0.6):
        os.write(master, key)
        pump(delay_after)

    # -------------------------------------------------------------
    # 1. Startup & Initial Audit
    # App starts directly into "checking" mode and lands on dashboard
    # -------------------------------------------------------------
    wait_for_pattern(b"Workstation Check Results", timeout=8.0)
    pump(2.2)  # Pause to let viewer absorb the dashboard and status pills

    # -------------------------------------------------------------
    # 2. Arrow Navigation through components
    # Using arrows to demonstrate moving through the table
    # -------------------------------------------------------------
    send_key(KEY_DOWN, 0.6)  # Row 1: herdr
    send_key(KEY_DOWN, 0.6)  # Row 2: ghostty
    send_key(KEY_DOWN, 0.6)  # Row 3: starship
    send_key(KEY_DOWN, 1.2)  # Row 4: opencode (status: UPDATE)

    # -------------------------------------------------------------
    # 3. Component Detail: Outdated component (opencode)
    # Shows unified "Update Available" section with green manual command
    # -------------------------------------------------------------
    send_key(KEY_ENTER, 1.0)  # Open detail view
    pump(2.2)  # Pause to see "Update Available" & copyable command

    # Scrolling within detail view
    send_key(KEY_DOWN, 0.8)  # Scroll down to Evidence
    send_key(KEY_DOWN, 0.8)  # Scroll down to References
    send_key(KEY_UP, 0.8)  # Scroll back up
    send_key(KEY_B, 1.2)  # Return to results dashboard

    # -------------------------------------------------------------
    # 4. Component Detail: Manual-only component (brew-outdated)
    # Scroll down to brew-outdated (7 steps down from row 4 to row 11)
    # -------------------------------------------------------------
    for _ in range(7):
        send_key(KEY_DOWN, 0.25)
    pump(1.0)
    send_key(KEY_ENTER, 1.0)  # Open brew-outdated detail
    pump(2.4)  # Pause to see manual commands for Homebrew formula & cask
    send_key(KEY_B, 1.2)  # Return to results dashboard

    # -------------------------------------------------------------
    # 5. Interactive Batch Selection (select_fixes)
    # Press 'f' on results dashboard to open selection screen
    # -------------------------------------------------------------
    send_key(KEY_F, 1.5)  # Enter select_fixes
    pump(1.8)  # Pause to see candidate list and checkboxes

    # Demonstrate selection: arrow down and Space to toggle/uncheck
    send_key(KEY_DOWN, 0.8)  # Highlight second candidate (pi-packages)
    send_key(
        KEY_SPACE, 1.2
    )  # Toggle Space: unchecks candidate (1 of 2 selected)
    send_key(KEY_UP, 0.8)  # Highlight first candidate (opencode)
    send_key(KEY_ENTER, 1.5)  # Confirm selection -> moves to Confirm Update
    pump(2.4)  # Pause to see the clean, safe preview with planned steps

    # -------------------------------------------------------------
    # 6. Execute Confirmed Action
    # Press 'y' to apply the update!
    # -------------------------------------------------------------
    send_key(KEY_Y, 1.0)
    # Wait for execution and verification to complete
    start_wait = time.time()
    while time.time() - start_wait < 25.0:
        pump(0.5)
        if b"Update Completed" in raw_buf:
            break
    pump(2.8)  # Pause on green Update Completed screen

    # -------------------------------------------------------------
    # 7. Return to Dashboard & Re-Audit
    # -------------------------------------------------------------
    send_key(KEY_ENTER, 1.0)  # Press Enter to re-audit
    start_wait = time.time()
    while time.time() - start_wait < 10.0:
        pump(0.5)
        if b"Workstation Check Results" in raw_buf:
            break
    pump(2.2)  # Pause to see opencode now marked OK!

    # -------------------------------------------------------------
    # 8. Manual Re-Check Demonstration
    # Press 'r' to trigger re-check on demand
    # -------------------------------------------------------------
    send_key(KEY_R, 1.0)
    start_wait = time.time()
    while time.time() - start_wait < 8.0:
        pump(0.5)
        if b"Workstation Check Results" in raw_buf:
            break
    pump(1.5)

    # -------------------------------------------------------------
    # 9. Update History Browser
    # Press 'h' to open Update History
    # -------------------------------------------------------------
    send_key(KEY_H, 1.5)  # Open history table
    pump(1.8)  # Pause to see history table with newly executed action at top

    # Arrow navigation through history records
    send_key(KEY_DOWN, 0.6)  # Row 1
    send_key(KEY_DOWN, 0.6)  # Row 2
    send_key(KEY_UP, 0.6)  # Row 1
    send_key(KEY_UP, 0.8)  # Top row (new action)
    send_key(KEY_ENTER, 1.0)  # View detailed action record
    pump(
        2.5
    )  # Pause to see Result, Installed Version, Target Version, Plan Steps

    # Return to history, then return to dashboard
    send_key(KEY_B, 1.0)  # Back to history list
    send_key(KEY_B, 1.2)  # Back to dashboard

    # -------------------------------------------------------------
    # 10. Clean Quit
    # -------------------------------------------------------------
    send_key(KEY_Q, 0.5)  # Quit application

    try:
        os.close(master)
        os.waitpid(pid, 0)
    except Exception:
        pass


if __name__ == "__main__":
    main()
