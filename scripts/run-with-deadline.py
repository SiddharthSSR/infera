#!/usr/bin/env python3
"""Run one command within an absolute epoch deadline and kill its process group on timeout."""

from __future__ import annotations

import os
import signal
import subprocess
import sys
import time


TERMINATION_MARGIN_SECONDS = 2.0


class RecoveryInterrupted(Exception):
    """Raised after the wrapper receives a controller termination signal."""

    def __init__(self, signum: int) -> None:
        super().__init__(signum)
        self.signum = signum


def terminate_and_wait(process: subprocess.Popen[bytes]) -> None:
    """Reap the whole child process group before returning to the coordinator."""
    if process.poll() is not None:
        process.wait()
        return

    # Do not let a second terminal signal interrupt cleanup and orphan a paid action.
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGHUP, signal.SIG_IGN)
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=1.0)
        return
    except subprocess.TimeoutExpired:
        pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait()


def main() -> int:
    if len(sys.argv) < 3:
        print("usage: run-with-deadline.py <deadline-epoch> <command> [args...]", file=sys.stderr)
        return 2
    try:
        deadline = int(sys.argv[1])
    except ValueError:
        return 2
    remaining = deadline - time.time()
    if remaining <= TERMINATION_MARGIN_SECONDS:
        print("ERROR: recovery command deadline exhausted", file=sys.stderr)
        return 124

    def interrupted(signum: int, _frame: object) -> None:
        raise RecoveryInterrupted(signum)

    handled_signals = {signal.SIGHUP, signal.SIGINT, signal.SIGTERM}
    previous_mask = signal.pthread_sigmask(signal.SIG_BLOCK, handled_signals)

    def prepare_child() -> None:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
        for signum in handled_signals:
            signal.signal(signum, signal.SIG_DFL)

    try:
        process = subprocess.Popen(
            sys.argv[2:],
            start_new_session=True,
            preexec_fn=prepare_child,
        )
        signal.signal(signal.SIGHUP, interrupted)
        signal.signal(signal.SIGINT, interrupted)
        signal.signal(signal.SIGTERM, interrupted)
    except BaseException:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
        raise
    try:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
        return process.wait(timeout=remaining - TERMINATION_MARGIN_SECONDS)
    except subprocess.TimeoutExpired:
        terminate_and_wait(process)
        print("ERROR: recovery command exceeded its absolute deadline", file=sys.stderr)
        return 124
    except RecoveryInterrupted as exc:
        terminate_and_wait(process)
        return 128 + exc.signum
    except KeyboardInterrupt:
        terminate_and_wait(process)
        return 130
    except BaseException:
        terminate_and_wait(process)
        raise


if __name__ == "__main__":
    raise SystemExit(main())
