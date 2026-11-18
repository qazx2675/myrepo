"""외부 컨트롤러용 인터페이스 (향후 CUDA 자율주행 코드가 붙는 곳).

두 가지 방식을 모두 지원한다.

1) 내비게이션 방식: obs(dict of numpy arrays) 만 보고 제어
   - 차선 중심선 미리보기(내 차 기준 좌표), 곡률, 제한속도, 주변 차량 상대위치/속도 등
   - 목적지 경로(route): 출구 차로 변경 + 진출로까지 이어지는 경로 좌표, 안내 문구/남은 거리
   - 주차장(level="parking")에서는 목표 주차칸/장애물 좌표 (sim/parking.py parking_observation)
2) 순수 화면 캡처 방식: frame (H, W, 3) uint8 RGB 1인칭 영상만 보고 제어
   - HUD 없이 도로 장면만 렌더링한 이미지 (기본 640x360)

행동(action)은 (throttle 0~1, brake 0~1, steer -1~1, +는 왼쪽) 이며 키보드 입력과 같은 경로로 들어간다.

    env = DrivingEnv(level="normal", vehicle="car", powertrain="ev", frame_size=(640, 360))
    obs, frame = env.reset()
    env.world.nav.set_destination(0)          # 목적지 설정 (내비 방식)
    while True:
        obs, frame = env.step((throttle, brake, steer))
"""
import math
import os

import numpy as np

from sim.parking import ParkingWorld
from sim.world import World

DT = 1.0 / 60.0
LEVELS = ("easy", "normal", "hard", "long", "parking")


def make_world(level="normal", vehicle="car", powertrain="auto", cars=None, seed=0, lanes=3):
    """level: easy/normal/hard(초/중/고급), long(8 km 직선 코스), parking(주차장)."""
    if level == "parking":
        return ParkingWorld(vehicle, powertrain, seed)
    if level == "long":   # 장거리 직선 코스는 중급 트래픽 설정 사용
        return World("normal", vehicle, powertrain, cars, seed, lanes, track="long")
    return World(level, vehicle, powertrain, cars, seed, lanes)


class DrivingEnv:
    def __init__(self, level="normal", vehicle="car", powertrain="auto", n_cars=None, n_lanes=3, seed=0,
                 frame_size=(640, 360), render_frames=True):
        self.level, self.vehicle, self.powertrain = level, vehicle, powertrain
        self.n_cars, self.n_lanes, self.seed = n_cars, n_lanes, seed
        self.frame_size = frame_size
        self.render_frames = render_frames
        self._surf = None
        self.world = None

    # ------------------------------------------------------------------
    def reset(self, seed=None):
        if seed is not None:
            self.seed = seed
        self.world = make_world(self.level, self.vehicle, self.powertrain, self.n_cars, self.seed, self.n_lanes)
        return self.observe(), self.render_frame()

    def step(self, action, substeps=1):
        """action 적용 후 substeps × 1/60 초 진행."""
        throttle, brake, steer = action
        w = self.world
        w.ego_mode = "manual"
        for _ in range(substeps):
            w.step(DT, float(np.clip(throttle, 0, 1)), float(np.clip(brake, 0, 1)),
                   float(np.clip(steer, -1, 1)))
        return self.observe(), self.render_frame()

    # ------------------------------------------------------------------
    def observe(self, preview=100.0, step=5.0, radius=150.0):
        """내비게이션 방식용 관측값. 좌표는 모두 내 차 기준 (x: 전방 +, y: 왼쪽 +) [m]."""
        w = self.world
        if not hasattr(w, "track"):
            return w.parking_observation()
        t, e = w.track, w.ego
        base = w.ego_observation()
        c, s = math.cos(e.yaw), math.sin(e.yaw)

        def local(x, y):
            dx, dy = x - e.x, y - e.y
            return np.stack([dx * c + dy * s, -dx * s + dy * c], -1).astype(np.float32)

        dists = np.arange(0.0, preview, step)
        road = w.ego_road()
        lane = base["lane"]
        lane_d = t.lane_d(lane) if e.road is None else 0.0
        cx, cy, _ = road.to_world(e.s + dists, lane_d)
        lx, ly, _ = road.to_world(e.s + dists, lane_d + t.lane_width / 2)
        rx, ry, _ = road.to_world(e.s + dists, lane_d - t.lane_width / 2)
        g = w.nav.guidance(e)
        route_lane = g["required_lane"] if g["required_lane"] is not None else lane
        qx, qy = w.nav.route_xy(e, dists, route_lane)

        rel_s = np.mod(w.s[1:] - e.s + t.L / 2, t.L) - t.L / 2
        near = np.where(np.abs(rel_s) < radius)[0] + 1
        others = np.zeros((len(near), 6), np.float32)
        if len(near):
            others[:, :2] = local(w.x[near], w.y[near])
            others[:, 2] = w.v[near]
            others[:, 3] = t.lane_of(w.d[near])
            others[:, 4] = w.length[near]
            others[:, 5] = w.width[near]

        return dict(
            speed=np.float32(e.vx),                        # [m/s] (후진이면 음수)
            steer_angle=np.float32(e.steer),               # [rad]
            max_steer=np.float32(e.max_steer()),           # steer=1 일 때 조향각 [rad]
            lane=lane, n_lanes=t.n_lanes, lane_width=t.lane_width,
            lateral_offset=np.float32(e.d - lane_d),          # 현재 차로 중심 대비 [m, 왼쪽 +]
            on_ramp=base["on_ramp"],
            heading_error=np.float32(base["heading_error"]),  # 도로 방향 대비 [rad]
            speed_limit=np.float32(base["speed_limit_kmh"] / 3.6),
            recommended_speed=np.float32(road.vref_at(e.s)),  # 곡선 대비 권장속도 [m/s]
            lane_center=local(cx, cy),                     # (P, 2) 현재 차로 중심선 미리보기
            lane_left=local(lx, ly),                       # (P, 2) 왼쪽 차선
            lane_right=local(rx, ry),                      # (P, 2) 오른쪽 차선
            route=local(qx, qy),                           # (P, 2) 목적지까지 경로 (출구 차로 → 진출로)
            nav=g,                                         # 목적지/남은거리/안내문구/필요차로/도착여부
            curvature=base["kappa_preview"].astype(np.float32),
            lead_gap=np.float32(base["lead_gap"] if base["lead_gap"] is not None else np.inf),
            lead_speed=np.float32(base["lead_v"] if base["lead_v"] is not None else 0.0),
            vehicles=others,   # (K, 6): x, y, speed, lane, length, width
            gear=e.gear, reverse=e.reverse, rpm=np.float32(e.rpm), powertrain=e.powertrain,
            battery_soc=np.float32(e.soc), vehicle_kind=e.kind,
            collisions=w.collisions, time=np.float32(w.time),
        )

    def render_frame(self, view="cockpit"):
        """순수 화면 캡처 방식용: HUD 없는 1인칭 RGB 이미지 (H, W, 3) uint8."""
        if not self.render_frames:
            return None
        import pygame
        from viz.render3d import draw_scene
        if self._surf is None:
            os.environ.setdefault("SDL_VIDEODRIVER", "dummy")
            pygame.display.init()
            pygame.font.init()
            self._surf = pygame.Surface(self.frame_size)
        draw_scene(self._surf, self.world, view)
        return np.ascontiguousarray(pygame.surfarray.array3d(self._surf).transpose(1, 0, 2))
