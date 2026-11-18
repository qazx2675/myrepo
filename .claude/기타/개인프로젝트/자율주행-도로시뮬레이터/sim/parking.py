"""주차장 시나리오 (자동주차 실험용).

- 직각 주차칸 한 줄(위쪽) + 평행 주차칸 한 줄(아래쪽), 대부분 주차된 차로 채워짐
- 목표 칸(초록색)에 넣으면 성공 판정: 차체 네 모서리가 칸 안 + 각도 허용치 이내 + 정지
- 주차된 차/벽과 충돌 감지
- 칸 크기는 내 차 크기에 맞춰 생성 (버스/트럭도 가능)

좌표: x 오른쪽, y 위쪽 [m]. 내 차는 sim/vehicle.py 동역학 + 후진 기어(G).
"""
import math
from dataclasses import dataclass

import numpy as np

from .vehicle import POWERTRAINS, Vehicle, load_specs
from .world import CAR_COLORS

KINDS = ("car", "bus", "truck")
WHITE, YELLOW = (235, 235, 235), (230, 190, 40)
ANGLE_TOL = math.radians(6.0)


def rect_corners(x, y, yaw, length, width):
    c, s = math.cos(yaw), math.sin(yaw)
    pts = [(length / 2, width / 2), (length / 2, -width / 2), (-length / 2, -width / 2), (-length / 2, width / 2)]
    return np.array([(x + a * c - b * s, y + a * s + b * c) for a, b in pts])


def rects_overlap(a, b):
    """분리축 정리(SAT)로 두 사각형(4x2 꼭짓점) 겹침 판정."""
    for poly in (a, b):
        for k in range(4):
            e = poly[(k + 1) % 4] - poly[k]
            n = np.array([-e[1], e[0]])
            pa, pb = a @ n, b @ n
            if pa.max() < pb.min() or pb.max() < pa.min():
                return False
    return True


def inside(poly, pts):
    """볼록 사각형 poly 안에 pts 가 모두 있는지."""
    c = poly.mean(axis=0)
    for k in range(4):
        e = poly[(k + 1) % 4] - poly[k]
        side_c = e[0] * (c[1] - poly[k][1]) - e[1] * (c[0] - poly[k][0])
        rel = pts - poly[k]
        side = e[0] * rel[:, 1] - e[1] * rel[:, 0]
        if (side * side_c < 0).any():
            return False
    return True


@dataclass
class Space:
    kind: str           # "perpendicular" / "parallel"
    x: float
    y: float
    yaw: float          # 칸의 길이 방향
    length: float
    width: float

    @property
    def corners(self):
        return rect_corners(self.x, self.y, self.yaw, self.length, self.width)


class ParkingLot:
    def __init__(self, car_len, car_w, rng, n_perp=10, n_par=5):
        pw, pd = car_w + 0.9, car_len + 0.8          # 직각 칸 폭/깊이
        ql, qw = car_len + 2.2, car_w + 0.6           # 평행 칸 길이/폭
        aisle = max(7.0, car_len * 1.3)               # 통로 폭
        width = max(n_perp * pw, n_par * ql) + 16.0
        height = pd + aisle + qw + 10.0
        self.rect = (0.0, 0.0, width, height)
        self.bounds = [(0, 0), (width, 0), (width, height), (0, height)]
        self.spaces = []
        x_start = 8.0
        for k in range(n_perp):
            self.spaces.append(Space("perpendicular", x_start + pw * (k + 0.5), height - 2.0 - pd / 2,
                                     math.pi / 2, pd, pw))
        for k in range(n_par):
            self.spaces.append(Space("parallel", x_start + ql * (k + 0.5), 2.0 + qw / 2, 0.0, ql, qw))
        self.aisle_y = height - 2.0 - pd - aisle / 2
        self.lines = []
        for sp in self.spaces:
            c = sp.corners
            if sp.kind == "perpendicular":   # 양옆 + 안쪽 선 (입구 쪽은 열림)
                self.lines += [(tuple(c[0]), tuple(c[1]), WHITE), (tuple(c[2]), tuple(c[3]), WHITE),
                               (tuple(c[0]), tuple(c[3]), WHITE)]
            else:
                self.lines += [(tuple(c[0]), tuple(c[3]), WHITE), (tuple(c[1]), tuple(c[2]), WHITE),
                               (tuple(c[2]), tuple(c[3]), WHITE)]
        self.lines.append(((4.0, self.aisle_y), (width - 4.0, self.aisle_y), YELLOW))
        # 비어 있을 칸: 직각 2칸, 평행 1칸
        perp = [i for i, s in enumerate(self.spaces) if s.kind == "perpendicular"]
        par = [i for i, s in enumerate(self.spaces) if s.kind == "parallel"]
        self.empty = sorted(list(rng.choice(perp, 2, replace=False)) + list(rng.choice(par, 1)))
        self.target = int(self.empty[0])


class ParkingWorld:
    """주차 시나리오. 렌더러/HUD 가 쓰는 배열 이름은 World 와 같다."""

    def __init__(self, vehicle="car", powertrain="auto", seed=0, specs=None):
        self.rng = np.random.default_rng(seed)
        self.specs = specs or load_specs()
        self.level = dict(label="PARKING", name="parking")
        self.fog_dist = 900.0
        self.ego_mode = "manual"
        self.mode_label = None
        self.time = 0.0
        self.collisions = 0
        self.parked_time = None
        self._build(vehicle, powertrain)

    def _build(self, vehicle, powertrain):
        sp = self.specs[vehicle]
        self.lot = lot = ParkingLot(sp["length"], sp["width"], self.rng)
        occupied = [i for i in range(len(lot.spaces)) if i not in lot.empty]
        n = len(occupied) + 1
        self.n = n
        self.kind = np.array([vehicle] + ["car" if sp["length"] < 6 else vehicle] * (n - 1), dtype=object)
        self.length, self.width, self.height = (np.zeros(n) for _ in range(3))
        self.x, self.y, self.yaw = (np.zeros(n) for _ in range(3))
        self.color = [(250, 200, 40)]
        for k, i in enumerate(occupied, start=1):
            s = lot.spaces[i]
            if self.kind[k] == "car":
                L, W, H = 4.6, 1.85, 1.45
            else:
                L, W, H = sp["length"], sp["width"], sp["height"]
            yaw = s.yaw + (math.pi if self.rng.random() < 0.5 else 0.0)
            self.x[k], self.y[k], self.yaw[k] = s.x + self.rng.uniform(-0.15, 0.15), s.y, yaw
            self.length[k], self.width[k], self.height[k] = L, W, H
            self.color.append(CAR_COLORS[self.rng.integers(len(CAR_COLORS))])
        self.accel = np.zeros(n)
        e = Vehicle(sp, vehicle, powertrain, 4.0 + sp["length"] / 2, lot.aisle_y, 0.0)
        self.ego = e
        self.length[0], self.width[0], self.height[0] = e.length, e.width, e.height
        self._colliding = set()
        self.parked_time = None
        self.t_start = self.time
        self._sync()

    def _sync(self):
        e = self.ego
        self.x[0], self.y[0], self.yaw[0] = e.x, e.y, e.yaw
        self.accel[0] = e.ax

    # ------------------------------------------------------------------
    def step(self, dt, throttle=0.0, brake=0.0, steer_in=0.0):
        e = self.ego
        e.step(throttle, brake, steer_in * e.max_steer(), dt)
        self.time += dt
        ec = rect_corners(e.x, e.y, e.yaw, e.length, e.width)
        hits = set()
        for k in range(1, self.n):
            if math.hypot(self.x[k] - e.x, self.y[k] - e.y) < (self.length[k] + e.length) / 2 + 1:
                if rects_overlap(ec, rect_corners(self.x[k], self.y[k], self.yaw[k], self.length[k], self.width[k])):
                    hits.add(k)
        x0, y0, x1, y1 = self.lot.rect
        wall = (ec[:, 0] < x0).any() or (ec[:, 0] > x1).any() or (ec[:, 1] < y0).any() or (ec[:, 1] > y1).any()
        if hits or wall:
            if (hits - self._colliding) or (wall and "wall" not in self._colliding):
                self.collisions += 1
            # 겹치지 않게 되돌리고 정지
            c, s = math.cos(e.yaw), math.sin(e.yaw)
            e.x -= (e.vx * c - e.vy * s) * dt * 1.5
            e.y -= (e.vx * s + e.vy * c) * dt * 1.5
            e.vx = e.vy = e.r = 0.0
        self._colliding = hits | ({"wall"} if wall else set())
        st = self.status()
        if st["parked"] and self.parked_time is None:
            self.parked_time = self.time - self.t_start
        self._sync()

    def status(self):
        """목표 칸 대비 위치/각도 오차와 성공 여부."""
        e, tgt = self.ego, self.lot.spaces[self.lot.target]
        ec = rect_corners(e.x, e.y, e.yaw, e.length, e.width)
        dyaw = (e.yaw - tgt.yaw + math.pi / 2) % math.pi - math.pi / 2   # 앞/뒤 방향 모두 허용
        c, s = math.cos(tgt.yaw), math.sin(tgt.yaw)
        dx, dy = e.x - tgt.x, e.y - tgt.y
        lon, lat = dx * c + dy * s, -dx * s + dy * c
        ok_in = inside(tgt.corners, ec)
        ok_ang = abs(dyaw) < ANGLE_TOL
        return dict(inside=ok_in, angle_ok=ok_ang, stopped=abs(e.vx) < 0.2,
                    parked=ok_in and ok_ang and abs(e.vx) < 0.2,
                    yaw_error=float(dyaw), lon_error=float(lon), lat_error=float(lat))

    def parking_observation(self):
        """외부(자동주차) 컨트롤러용: 모든 좌표는 내 차 기준 (x 전방, y 왼쪽) [m]."""
        e = self.ego
        c, s = math.cos(e.yaw), math.sin(e.yaw)

        def local(p):
            d = np.asarray(p) - np.array([e.x, e.y])
            return np.stack([d[..., 0] * c + d[..., 1] * s, -d[..., 0] * s + d[..., 1] * c], -1).astype(np.float32)

        tgt = self.lot.spaces[self.lot.target]
        obstacles = np.array([rect_corners(self.x[k], self.y[k], self.yaw[k], self.length[k], self.width[k])
                              for k in range(1, self.n)])
        return dict(
            speed=np.float32(e.vx), steer_angle=np.float32(e.steer),
            max_steer=np.float32(math.radians(e.spec["max_steer_deg"])),
            reverse=e.reverse, length=e.length, width=e.width, wheelbase=e.wheelbase,
            target_kind=tgt.kind, target_corners=local(tgt.corners),
            target_center=local([tgt.x, tgt.y]),
            target_yaw=np.float32((tgt.yaw - e.yaw + math.pi) % (2 * math.pi) - math.pi),
            obstacles=local(obstacles) if len(obstacles) else np.zeros((0, 4, 2), np.float32),
            lot_bounds=local(np.array(self.lot.bounds)),
            collisions=self.collisions, **self.status(),
        )

    # ------------------------------------------------------------------
    def next_target(self):
        lot = self.lot
        k = lot.empty.index(lot.target)
        lot.target = int(lot.empty[(k + 1) % len(lot.empty)])
        self.parked_time = None
        self.t_start = self.time

    def reset_ego(self):
        self._build(self.ego.kind, self.ego.powertrain)

    def cycle_vehicle(self):
        self._build(KINDS[(KINDS.index(self.ego.kind) + 1) % len(KINDS)], self.ego.powertrain)

    def cycle_powertrain(self):
        pts = POWERTRAINS
        self.set_vehicle(powertrain=pts[(pts.index(self.ego.powertrain) + 1) % len(pts)])

    def set_vehicle(self, kind=None, powertrain=None):
        """차종/파워트레인/제원 변경. 차종이 바뀌면 주차칸 크기가 달라지므로 주차장을 다시 만든다."""
        e = self.ego
        if kind and kind != e.kind:
            self._build(kind, powertrain or e.powertrain)
            return
        nv = Vehicle(self.specs[e.kind], e.kind, powertrain or e.powertrain, e.x, e.y, e.yaw)
        self.ego = nv
        self.length[0], self.width[0], self.height[0] = nv.length, nv.width, nv.height

    def draw_hud(self, hud, surf, fps, paused, view):
        st = self.status()
        tgt = self.lot.spaces[self.lot.target]
        lines = [f"PARKING  target: {tgt.kind}  (N = next space)",
                 f"GEAR {'R' if self.ego.reverse else 'D'}   (G = toggle when stopped)",
                 f"LON {st['lon_error']:+6.2f} m  LAT {st['lat_error']:+6.2f} m",
                 f"YAW {math.degrees(st['yaw_error']):+6.1f} deg (tol {math.degrees(ANGLE_TOL):.0f})",
                 f"INSIDE {'YES' if st['inside'] else 'no '}   COLLISION {self.collisions}",
                 f"VIEW {view}   {fps:4.0f} FPS" + ("   [PAUSED]" if paused else "")]
        hud.lines_box(surf, lines, w=380)
        if self.parked_time is not None:
            W, H = surf.get_size()
            hud.text(surf, f"PARKED!  {self.parked_time:.1f} s", hud.f_big, (W // 2, 50), (90, 240, 120),
                     center=True)
