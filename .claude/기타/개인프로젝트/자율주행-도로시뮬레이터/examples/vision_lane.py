"""순수 화면 캡처 방식 기준 컨트롤러 예제 (CPU, numpy).

1인칭 RGB 프레임에서 흰색/황색 차선 픽셀을 찾아 차로 중심을 추정하고 조향한다.
쓰는 정보는 frame 과 속도계(obs["speed"])뿐이다. 나중에 이 이미지 처리 부분을
CUDA 커널(또는 CuPy)로 옮기면 된다.

    python examples/vision_lane.py --seconds 120 --save-frames out/   # 화면 없이 통계 + 프레임 저장
    python main.py --controller examples/vision_lane.py                # 창에서 보기
"""
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

TARGET_KMH = 70.0


def lane_mask(frame):
    r, g, b = (frame[..., i].astype(np.int16) for i in range(3))
    white = (r > 205) & (g > 205) & (b > 205) & (np.abs(r - b) < 25)
    yellow = (r > 190) & (g > 150) & (b < 110)
    return white | yellow


class Controller:
    needs_frame = True

    def __init__(self):
        self.prev_err = 0.0

    def lane_center(self, frame):
        """화면 하단 띠에서 좌/우 차선 x 좌표 -> 차로 중심의 정규화 오차 (-1~1, + = 중심이 왼쪽)."""
        H, W, _ = frame.shape
        mask = lane_mask(frame)
        cx = W / 2
        centers, weights = [], []
        for y0, y1, wgt in ((0.62, 0.72, 1.0), (0.54, 0.62, 0.6)):
            band = mask[int(H * y0):int(H * y1)]
            for row in band[::2]:
                xs = np.nonzero(row)[0]
                left, right = xs[xs < cx], xs[xs >= cx]
                if len(left) and len(right):
                    centers.append((left.max() + right.min()) / 2)
                    weights.append(wgt)
        if not centers:
            return None
        c = np.average(centers, weights=weights)
        return (cx - c) / (W / 2)

    def __call__(self, obs, frame):
        err = self.lane_center(frame)
        if err is None:
            err = self.prev_err
        steer = 2.2 * err + 3.0 * (err - self.prev_err)
        self.prev_err = err
        v = float(obs["speed"]) * 3.6
        throttle = float(np.clip((TARGET_KMH - v) * 0.1, 0, 1))
        brake = float(np.clip((v - TARGET_KMH - 5) * 0.05, 0, 1))
        return throttle, brake, float(np.clip(steer, -1, 1))


if __name__ == "__main__":
    import argparse
    from env import DrivingEnv

    ap = argparse.ArgumentParser()
    ap.add_argument("--seconds", type=float, default=120)
    ap.add_argument("--save-frames", help="10초마다 프레임을 PNG로 저장할 폴더")
    a = ap.parse_args()
    env = DrivingEnv(frame_size=(640, 360))
    obs, frame = env.reset()
    ctl = Controller()
    max_off = 0.0
    n = int(a.seconds * 20)  # 20 Hz 제어 (프레임당 3 스텝)
    for k in range(n):
        obs, frame = env.step(ctl(obs, frame), substeps=3)
        max_off = max(max_off, abs(float(obs["lateral_offset"])))
        if a.save_frames and k % 200 == 0:
            import pygame
            os.makedirs(a.save_frames, exist_ok=True)
            pygame.image.save(pygame.surfarray.make_surface(frame.transpose(1, 0, 2)),
                              os.path.join(a.save_frames, f"frame_{k // 20:04d}s.png"))
    w = env.world
    print(f"distance {w.odometer:.0f} m ({w.odometer / w.track.L:.2f} laps), speed {obs['speed'] * 3.6:.0f} km/h, lane {obs['lane'] + 1}")
    print(f"max lateral offset {max_off:.2f} m, collisions {w.collisions}, rail hits {w.rail_hits}")
