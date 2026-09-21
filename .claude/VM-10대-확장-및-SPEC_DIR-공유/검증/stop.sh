#!/usr/bin/env bash
for p in /tmp/sim_*.pid; do [ -f "$p" ] && kill "$(cat "$p")" 2>/dev/null; rm -f "$p"; done; true
