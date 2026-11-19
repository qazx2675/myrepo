"""기본 동작 테스트: python -m pytest tests -q"""
import math
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from env import DrivingEnv, make_world  # noqa: E402
from sim.track import Track  # noqa: E402

DT = 1 / 60


def test_tracks_close_and_project():
    for lv in ("easy", "normal", "hard", "long"):
        t = Track(level=lv)
        assert t.closure_error < 1e-3
        assert len(t.ramps) >= 2
        s, d = np.array([123.0, 2000.0]), np.array([1.5, -3.6])
        x, y, _ = t.to_world(s, d)
        for k in range(2):
            s2, d2 = t.project(x[k], y[k], s_hint=s[k])
            assert abs(s2 - s[k]) < 0.2 and abs(d2 - d[k]) < 0.05


def test_traffic_no_overlap_and_autopilot_reaches_destination():
    w = make_world("normal")
    w.ego_mode = "auto"
    w.nav.set_destination(0)
    arrived = False
    for k in range(60 * 150):
        w.step(DT)
        if k % 60 == 0:
            assert w.ai_overlaps() == 0
        arrived |= w.nav.guidance(w.ego)["arrived"]
    assert arrived and w.collisions == 0 and w.rail_hits == 0


def test_powertrains_accelerate():
    for pt in ("auto", "manual", "ev", "hybrid"):
        w = make_world("long", "car", pt, cars=0)
        for _ in range(60 * 15):
            if w.ego.rpm > 6000:      # 수동은 직접 변속
                w.ego.shift(+1)
            w.step(DT, 1.0, 0.0, 0.0)
        assert 100 in w.accel_timer.results, pt


def test_parking_success_and_reverse():
    env = DrivingEnv(level="parking", render_frames=False)
    env.reset()
    w = env.world
    w.ego.toggle_reverse()
    for _ in range(30):
        env.step((0.5, 0.0, 0.0))
    assert w.ego.vx < 0
    tgt = w.lot.spaces[w.lot.target]
    e = w.ego
    e.reverse, e.vx = False, 0.0
    e.x, e.y, e.yaw = tgt.x, tgt.y, tgt.yaw + math.pi
    w.step(DT)
    assert w.status()["parked"]


def test_env_frame_and_obs():
    env = DrivingEnv(frame_size=(320, 180))
    obs, frame = env.reset()
    assert frame.shape == (180, 320, 3) and frame.dtype == np.uint8
    assert obs["route"].shape[1] == 2 and "nav" in obs


if __name__ == "__main__":   # pytest 없이도 실행: python tests/test_sim.py
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print("OK  ", name)
