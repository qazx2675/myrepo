"""내비게이션(경로 데이터) 방식 기준 컨트롤러 예제 (CPU).

obs 의 경로(route) 좌표 + 선행차 거리만으로 차선유지·차간거리 유지·목적지(출구) 진출.
CUDA 버전을 만들 때 이 클래스와 같은 시그니처로 교체하면 된다.

    python examples/nav_follow.py --seconds 300      # 화면 없이 통계
    python main.py --controller examples/nav_follow.py   # 창에서 보기
"""
import math
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from sim.controllers import idm_accel  # noqa: E402

WHEELBASE = 2.8


class Controller:
    needs_frame = False

    def __call__(self, obs, frame):
        v = float(obs["speed"])
        # 횡방향: 경로 위 전방 ld 지점을 향하는 pure pursuit (목적지 없으면 현재 차로 중심)
        ld = 6.0 + 0.8 * v
        pts = obs["route"]
        k = int(np.argmin(np.abs(np.hypot(pts[:, 0], pts[:, 1]) - ld)))
        x, y = float(pts[k, 0]), float(pts[k, 1])
        delta = math.atan2(2.0 * WHEELBASE * y, x * x + y * y)
        steer = delta / max(float(obs["max_steer"]), 1e-3)
        # 종방향: IDM (권장속도까지, 선행차 있으면 차간거리 유지)
        a = float(idm_accel(v, float(obs["recommended_speed"]), float(obs["lead_gap"]),
                            float(obs["lead_speed"]), 1.5, 2.0, 1.5, 3.0))
        throttle = max(0.0, a / 2.5)
        brake = max(0.0, -a / 7.0)
        return throttle, brake, steer


if __name__ == "__main__":
    import argparse
    from env import DrivingEnv

    ap = argparse.ArgumentParser()
    ap.add_argument("--seconds", type=float, default=300)
    a = ap.parse_args()
    env = DrivingEnv(render_frames=False)
    obs, frame = env.reset()
    env.world.nav.set_destination(0)
    ctl = Controller()
    max_off = 0.0
    for _ in range(int(a.seconds * 60)):
        obs, frame = env.step(ctl(obs, frame))
        max_off = max(max_off, abs(float(obs["lateral_offset"])))
    w = env.world
    print(f"distance {w.odometer:.0f} m ({w.odometer / w.track.L:.2f} laps), speed {obs['speed'] * 3.6:.0f} km/h")
    print(f"max lateral offset {max_off:.2f} m, collisions {w.collisions}, rail hits {w.rail_hits}")
    print("nav:", obs["nav"]["instruction"])
