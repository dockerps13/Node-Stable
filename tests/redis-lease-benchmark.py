#!/usr/bin/env python3
"""Isolated Redis Lua lease benchmark. No production Redis access.

Measures renewal latency and throughput at different active-lease cardinalities.
This is a short CI capacity probe, NOT a 24-hour soak or HTTP/Laravel benchmark.
Only uses Python standard library and a disposable Redis service.
"""
import argparse
import concurrent.futures
import json
import math
import os
from pathlib import Path
import socket
import statistics
import threading
import time
import uuid


class Redis:
    def __init__(self, host, port):
        self.sock = socket.create_connection((host, port), timeout=5)
        self.sock.settimeout(15)
        self.reader = self.sock.makefile("rb")

    def close(self):
        self.reader.close()
        self.sock.close()

    def command(self, *args):
        chunks = [("*%d\r\n" % len(args)).encode()]
        for arg in args:
            payload = str(arg).encode() if not isinstance(arg, bytes) else arg
            chunks.extend([("$%d\r\n" % len(payload)).encode(), payload, b"\r\n"])
        self.sock.sendall(b"".join(chunks))
        return self._read()

    def _read(self):
        line = self.reader.readline()
        if not line.endswith(b"\r\n"):
            raise RuntimeError("Redis response truncated")
        kind, payload = line[:1], line[1:-2]
        if kind == b"+":
            return payload.decode()
        if kind == b"-":
            raise RuntimeError("Redis error: " + payload.decode(errors="replace"))
        if kind == b":":
            return int(payload)
        if kind == b"$":
            length = int(payload)
            if length == -1:
                return None
            data = self.reader.read(length + 2)
            if len(data) != length + 2 or not data.endswith(b"\r\n"):
                raise RuntimeError("Redis bulk reply truncated")
            return data[:-2]
        if kind == b"*":
            return [self._read() for _ in range(int(payload))]
        raise RuntimeError("unexpected Redis reply type")


def percentile(sorted_values, pct):
    if not sorted_values:
        return None
    return round(sorted_values[min(len(sorted_values) - 1,
                                  math.ceil(len(sorted_values) * pct) - 1)], 3)


def run_case(host, port, script_sha, prefix, owners, workers, seconds):
    key = prefix + ":owners-" + str(owners)
    setup = Redis(host, port)
    try:
        # Each owner is unique but there are only four distinct IP slots.
        # This drives worst-case HGETALL scanning while preserving quota parity.
        for n in range(owners):
            reply = setup.command("EVALSHA", script_sha, 1, key,
                                  "acquire", "owner-%d" % n,
                                  "198.18.0.%d" % (n % 4 + 1), 4, 120000)
            if reply[0] != 1 or reply[1] > 4:
                raise AssertionError("invalid setup admission: %r" % (reply,))
    finally:
        setup.close()
    samples = []
    failures = []
    lock = threading.Lock()
    start = time.monotonic()
    end = start + seconds

    def worker(wid):
        conn = Redis(host, port)
        local = []
        try:
            index = wid
            while time.monotonic() < end:
                stamp = time.perf_counter_ns()
                reply = conn.command("EVALSHA", script_sha, 1, key,
                                     "renew", "owner-%d" % (index % owners),
                                     "", 4, 120000)
                local.append((time.perf_counter_ns() - stamp) / 1_000_000)
                if reply[0] != 1 or reply[1] != 4:
                    raise AssertionError("invalid renewal: %r" % (reply,))
                index += workers
        except Exception as exc:
            with lock:
                failures.append(str(exc))
        finally:
            conn.close()
            with lock:
                samples.extend(local)

    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
            list(pool.map(worker, range(workers)))
        if failures or not samples:
            raise RuntimeError("benchmark failures=%r operations=%d" %
                               (failures, len(samples)))
        elapsed = time.monotonic() - start
        samples.sort()
        result = {"active_owner_leases": owners, "distinct_ips": 4,
                  "workers": workers, "elapsed_s": round(elapsed, 3),
                  "operations": len(samples),
                  "renewals_per_second": round(len(samples) / elapsed, 2),
                  "latency_ms": {"p50": percentile(samples, .5),
                                 "p95": percentile(samples, .95),
                                 "p99": percentile(samples, .99),
                                 "max": round(samples[-1], 3)}}
        print(json.dumps(result, sort_keys=True), flush=True)
        return result
    finally:
        cleanup = Redis(host, port)
        try:
            cleanup.command("DEL", key)
        finally:
            cleanup.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=6379)
    parser.add_argument("--duration", type=float, default=8)
    parser.add_argument("--workers", type=int, default=12)
    parser.add_argument("--cardinalities", default="32,256")
    parser.add_argument("--output", default="redis-lease-benchmark.json")
    args = parser.parse_args()
    counts = [int(x) for x in args.cardinalities.split(",")]
    if not counts or any(n < 4 or n > 4096 for n in counts):
        parser.error("cardinalities must be between 4 and 4096")
    if not 1 <= args.workers <= 128 or not 1 <= args.duration <= 3600:
        parser.error("workers or duration outside safety bounds")
    script = (Path(__file__).resolve().parents[1] /
              "internal/globaladmission/redis_lease.lua").read_bytes()
    admin = Redis(args.host, args.port)
    try:
        sha = admin.command("SCRIPT", "LOAD", script)
    finally:
        admin.close()
    prefix = "ga-bench-%s-%s" % (os.getpid(), uuid.uuid4().hex)
    results = [run_case(args.host, args.port, sha, prefix, n,
                        args.workers, args.duration) for n in counts]
    output = {"scope": "Redis Lua only; excludes HTTP/Laravel and node binaries",
              "cases": results}
    Path(args.output).write_text(json.dumps(output, indent=2) + "\n")
    print("Saved " + args.output)


if __name__ == "__main__":
    main()
