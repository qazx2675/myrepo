"""1인칭(운전석) / 3인칭(추적) 원근 투영 렌더러. 평평한 지면 가정, pygame 폴리곤만 사용.

운전 감각 연출(CameraFX): 속도에 따른 시야각 확장, 가속/제동 시 카메라 젖힘/쏠림,
코너 안쪽을 먼저 보는 시선, 고속 미세 진동, 차체 롤/노즈다이브(3인칭).
"""
import math

import numpy as np
import pygame

SKY_TOP = np.array([70, 130, 200])
SKY_BOT = np.array([185, 215, 235])
GRASS_A = np.array([88, 140, 60])
GRASS_B = np.array([78, 128, 54])
ASPHALT = np.array([95, 97, 100])
SHOULDER = np.array([125, 125, 122])
WHITE = np.array([235, 235, 235])
YELLOW = np.array([230, 190, 40])
RAIL = np.array([190, 195, 200])
FOG = np.array([185, 210, 225])
LOT = np.array([105, 107, 110])

_fog_dist = 520.0


def _fog(color, depth):
    k = min(1.0, max(0.0, depth / _fog_dist)) ** 1.3
    return tuple(int(c) for c in np.asarray(color) * (1 - k) + FOG * k)


class CameraFX:
    """가속도/조향을 부드럽게 따라가는 카메라·차체 연출 상태."""

    def __init__(self):
        self.ax = self.ay = self.look = 0.0
        self.t = 0.0
        self.enabled = True

    def update(self, ego, dt):
        k = 1 - math.exp(-dt / 0.25)
        ay = ego.vx * ego.r
        self.ax += (max(-9, min(9, ego.ax)) - self.ax) * k
        self.ay += (max(-9, min(9, ay)) - self.ay) * k
        look = max(-0.14, min(0.14, ego.steer * 1.5 + ego.r * 0.2))
        self.look += (look - self.look) * (1 - math.exp(-dt / 0.4))
        self.t += dt

    # 차체 연출 (라디안): 제동 시 앞이 숙여지고(+), 코너 바깥으로 기울어짐
    def body_pitch(self):
        return max(-0.05, min(0.05, -self.ax * 0.006)) if self.enabled else 0.0

    def body_roll(self):
        return max(-0.06, min(0.06, self.ay * 0.008)) if self.enabled else 0.0


class Camera:
    def __init__(self, x, y, h, yaw, pitch, W, H, fov_deg=70.0):
        self.x, self.y, self.h = x, y, h
        self.c, self.s = math.cos(yaw), math.sin(yaw)
        self.cp, self.sp = math.cos(pitch), math.sin(pitch)
        self.W, self.H = W, H
        self.focal = (W / 2) / math.tan(math.radians(fov_deg) / 2)
        self.horizon = H / 2 - self.focal * math.tan(pitch)

    def project(self, x, y, z):
        """월드 좌표 배열 -> (u, v, depth)."""
        dx, dy = x - self.x, y - self.y
        f = dx * self.c + dy * self.s
        left = -dx * self.s + dy * self.c
        zr = z - self.h
        depth = f * self.cp - zr * self.sp
        up = f * self.sp + zr * self.cp
        dsafe = np.where(depth > 1e-3, depth, 1e-3)
        u = self.W / 2 - self.focal * left / dsafe
        v = self.H / 2 - self.focal * up / dsafe
        return u, v, depth


def make_camera(ego, mode, W, H, fx=None):
    yaw, pitch_fx, fov, dh = ego.yaw, 0.0, 66.0, 0.0
    if fx is not None and fx.enabled:
        kmh = abs(ego.vx) * 3.6
        fov += min(24.0, kmh * 0.09)                       # 다이내믹 FOV
        pitch_fx = -fx.ax * 0.004                          # 가속: 젖혀짐 / 제동: 쏠림
        yaw += fx.look                                     # 코너 안쪽 시선
        amp = 0.0012 * min(1.0, (kmh / 120.0) ** 2)        # 고주파·저진폭 진동
        pitch_fx += amp * (math.sin(fx.t * 41.0) + 0.6 * math.sin(fx.t * 67.0 + 1.3))
        dh = amp * 20 * math.sin(fx.t * 53.0 + 0.4)
    c, s = math.cos(ego.yaw), math.sin(ego.yaw)
    if mode == "cockpit":
        fwd = ego.length * 0.25 if ego.kind != "car" else 0.2
        return Camera(ego.x + fwd * c, ego.y + fwd * s, ego.spec["eye_height"] + dh, yaw,
                      0.02 + pitch_fx, W, H, fov)
    back = 6.0 + ego.length
    return Camera(ego.x - back * c, ego.y - back * s, 2.4 + ego.height * 0.9 + dh, ego.yaw + 0.5 * (yaw - ego.yaw),
                  0.10 + 0.5 * pitch_fx, W, H, fov)


def _quad(surf, color, u, v, i, a, b):
    pygame.draw.polygon(surf, color, [(u[i, a], v[i, a]), (u[i, b], v[i, b]),
                                      (u[i + 1, b], v[i + 1, b]), (u[i + 1, a], v[i + 1, a])])


def _sky_ground(surf, cam, ground, yaw):
    W, H = surf.get_size()
    hz = int(cam.horizon)
    for yy in range(0, max(hz, 1), 4):
        k = yy / max(hz, 1)
        pygame.draw.rect(surf, tuple(int(c) for c in SKY_TOP * (1 - k) + SKY_BOT * k), (0, yy, W, 4))
    # 방위각에 고정된 구름과 먼 산 (회전하면 함께 움직임)
    us = np.arange(0, W + 8, 8)
    bearing = yaw + np.arctan((W / 2 - us) / cam.focal)
    for k in range(7):
        b0 = k * 2 * math.pi / 7 + 0.4
        db = (b0 - yaw + math.pi) % (2 * math.pi) - math.pi
        if abs(db) < 1.0:
            cu = W / 2 - cam.focal * math.tan(db)
            cv = hz - cam.focal * (0.12 + 0.05 * (k % 3))
            for j in range(3):
                pygame.draw.ellipse(surf, (240, 244, 248), (cu - 60 + j * 35, cv - 12 + (j % 2) * 6, 80, 26))
    for amp, freq, col, base in ((0.055, 1.0, (120, 145, 165), 0.0), (0.03, 2.3, (95, 125, 110), 0.004)):
        hgt = amp * (0.55 + 0.25 * np.sin(3 * freq * bearing + 1.0) + 0.2 * np.sin(7 * freq * bearing + 2.0)
                     + 0.1 * np.sin(13 * freq * bearing))
        pts = [(0, hz + 2)] + [(float(uu), hz - cam.focal * (h - base)) for uu, h in zip(us, hgt)] + [(W, hz + 2)]
        pygame.draw.polygon(surf, col, pts)
    pygame.draw.rect(surf, _fog(ground, 300), (0, hz, W, H - hz))


def draw_scene(surf, world, mode, fx=None):
    global _fog_dist
    W, H = surf.get_size()
    e = world.ego
    cam = make_camera(e, mode, W, H, fx)
    _fog_dist = world.fog_dist
    objs = []
    if hasattr(world, "lot"):
        _sky_ground(surf, cam, GRASS_A, math.atan2(cam.s, cam.c))
        _draw_lot(surf, world, cam)
    else:
        _sky_ground(surf, cam, GRASS_A, math.atan2(cam.s, cam.c))
        _draw_roads(surf, world, cam, objs)

    for i in range(world.n):
        if i == 0 and mode == "cockpit":
            continue
        _, _, dpt = cam.project(world.x[i], world.y[i], 0.0)
        if 0.5 < dpt < 450 or (i == 0 and dpt > 0.5):
            objs.append((float(dpt), "car", i, None))

    objs.sort(key=lambda o: -o[0])
    for dpt, kind, a, b in objs:
        if kind == "poly":
            pygame.draw.polygon(surf, a, b)
        elif kind == "tree":
            sc = cam.focal / dpt
            trunk_h, crown_r = a[5] * sc, a[4] * sc
            bu, bv = b
            pygame.draw.rect(surf, _fog(np.array([90, 65, 40]), dpt),
                             (bu - max(1, 0.25 * sc), bv - trunk_h * 0.55, max(2, 0.5 * sc), trunk_h * 0.55))
            pygame.draw.circle(surf, _fog(np.array([45, 100, 45]), dpt),
                               (int(bu), int(bv - trunk_h * 0.65)), max(1, int(crown_r)))
        elif kind == "building":
            col = BUILDING_COLORS[int(a[7])]
            _, _, psi = world.track.pose(a[0])
            _draw_box(surf, cam, a[2], a[3], float(psi), -a[4] / 2, a[4] / 2, a[5] / 2, 0.0, a[6], col, dpt)
        elif kind == "sign":
            _draw_sign(surf, cam, world.track, a, dpt)
        else:
            _draw_car(surf, cam, world, a, dpt, fx if a == 0 else None)

    if mode == "cockpit":
        _draw_cockpit(surf, e)
    return cam


# ----------------------------------------------------------------------
def _draw_roads(surf, world, cam, objs):
    t = world.track
    s_cam, _ = t.project(cam.x, cam.y, s_hint=world.ego_s_main)

    # --- 본선 행(row) 샘플: 가까울수록 촘촘하게 ---
    offs = np.concatenate([np.arange(0.5, 30, 1.0), np.arange(30, 120, 2.5), np.arange(120, 480, 6.0)])
    s_rows = s_cam + offs
    hw, sh, rd = t.half_width, t.shoulder, t.rail_d
    cols = np.array([hw + 90, hw + sh, hw, hw - 0.15, hw - 0.35,
                     -hw + 0.35, -hw + 0.15, -hw, -hw - sh, -hw - 90])
    X, Y, _ = t.to_world(s_rows[:, None], cols[None, :])
    u, v, dep = cam.project(X, Y, 0.0)
    ok = (dep > 0.3).all(axis=1)
    R = len(s_rows)
    row_depth = dep.min(axis=1)

    # 층 1: 잔디 (줄무늬로 속도감)
    for i in range(R - 2, -1, -1):
        if ok[i] and ok[i + 1]:
            g = GRASS_A if int(s_rows[i] // 20) % 2 == 0 else GRASS_B
            col = _fog(g, row_depth[i])
            _quad(surf, col, u, v, i, 0, 1)
            _quad(surf, col, u, v, i, 8, 9)

    # 층 1.5: 진출로
    for r in t.ramps:
        u0 = (s_cam - r.s0) % t.L
        if u0 < r.L + 50 or (r.s0 - s_cam) % t.L < 480:
            _draw_ramp(surf, r, cam, objs)

    # 층 2: 갓길 + 차도 + 가장자리 차선
    for i in range(R - 2, -1, -1):
        if not (ok[i] and ok[i + 1]):
            continue
        dpt = row_depth[i]
        _quad(surf, _fog(SHOULDER, dpt), u, v, i, 1, 2)
        _quad(surf, _fog(SHOULDER, dpt), u, v, i, 7, 8)
        _quad(surf, _fog(ASPHALT, dpt), u, v, i, 2, 7)
        _quad(surf, _fog(YELLOW, dpt), u, v, i, 3, 4)   # 1차로 왼쪽 실선(황색)
        _quad(surf, _fog(WHITE, dpt), u, v, i, 5, 6)    # 오른쪽 실선

    # 층 3: 차로 구분 점선 (8 m 선, 12 m 간격)
    first = math.ceil(s_cam / 20.0) * 20.0
    starts = first + 20.0 * np.arange(24)
    for k in range(1, t.n_lanes):
        dk = hw - k * t.lane_width
        ss = np.stack([starts, starts, starts + 8, starts + 8], 1)
        dd = np.array([dk + 0.08, dk - 0.08, dk - 0.08, dk + 0.08])[None, :].repeat(len(starts), 0)
        xx, yy, _ = t.to_world(ss, dd)
        du, dv, ddp = cam.project(xx, yy, 0.0)
        for j in range(len(starts) - 1, -1, -1):
            if (ddp[j] > 0.3).all():
                pygame.draw.polygon(surf, _fog(WHITE, ddp[j].min()), list(zip(du[j], dv[j])))

    # 세로 물체: 가드레일(진출로 분기 구간은 오른쪽 개방), 나무
    rail_rows = np.where(offs < 260)[0]
    gap = np.array([t.in_exit_zone(s, 60.0) for s in s_rows[rail_rows]])
    for side in (1.0, -1.0):
        rx, ry, _ = t.to_world(s_rows[rail_rows], side * rd)
        _rail_objs(cam, rx, ry, objs, skip=gap if side < 0 else None)
    for row in t.buildings[np.mod(t.buildings[:, 0] - s_cam, t.L) < 600]:
        _, _, bd = cam.project(row[2], row[3], 0.0)
        if bd > 5.0:
            objs.append((float(bd), "building", row, None))
    for sg in t.signs:
        if (sg[0] - s_cam) % t.L < 400:
            sx, sy, _ = t.to_world(sg[0], -(t.rail_d + 1.0))
            _, _, sd = cam.project(sx, sy, 0.0)
            if sd > 2.0:
                objs.append((float(sd), "sign", sg, None))
    tr = t.trees
    ahead = np.mod(tr[:, 0] - s_cam, t.L)
    for row in tr[ahead < 450]:
        bu, bv, bd = cam.project(row[2], row[3], 0.0)
        if bd > 1.0:
            objs.append((float(bd), "tree", row, (float(bu), float(bv))))


def _rail_objs(cam, rx, ry, objs, skip=None):
    lu, lv, ld = cam.project(rx, ry, 0.35)
    tu, tv, _ = cam.project(rx, ry, 0.8)
    for a in range(len(rx) - 1):
        if skip is not None and (skip[a] or skip[a + 1]):
            continue
        if ld[a] > 0.5 and ld[a + 1] > 0.5:
            objs.append((ld[a], "poly", _fog(RAIL, ld[a]),
                         [(lu[a], lv[a]), (lu[a + 1], lv[a + 1]), (tu[a + 1], tv[a + 1]), (tu[a], tv[a])]))


def _draw_ramp(surf, r, cam, objs):
    ss = np.arange(0.0, r.L, 3.0)
    hw = r.half_width
    cols = np.array([hw + 1.0, hw, hw - 0.2, -hw + 0.2, -hw, -hw - 1.0])
    X, Y, _ = r.to_world(ss[:, None], cols[None, :])
    u, v, dep = cam.project(X, Y, 0.0)
    ok = (dep > 0.3).all(axis=1) & (dep.min(axis=1) < 480)
    for i in range(len(ss) - 2, -1, -1):
        if ok[i] and ok[i + 1]:
            dpt = dep[i].min()
            _quad(surf, _fog(SHOULDER, dpt), u, v, i, 0, 5)
            _quad(surf, _fog(ASPHALT, dpt), u, v, i, 1, 4)
            if ss[i] > r.taper * 0.6:
                _quad(surf, _fog(WHITE, dpt), u, v, i, 1, 2)
            _quad(surf, _fog(WHITE, dpt), u, v, i, 3, 4)
    near = ss[ok]
    if len(near):
        sr = np.arange(max(near.min(), 0.0), min(near.max(), r.L), 4.0)
        for side, start in ((1.0, r.taper + 20), (-1.0, 0.0)):
            q = sr[sr >= start]
            if len(q) > 1:
                rx, ry, _ = r.to_world(q, side * r.rail_d)
                _rail_objs(cam, rx, ry, objs)


def _draw_lot(surf, world, cam):
    """주차장 바닥 + 주차선."""
    lot = world.lot
    corners = np.array(lot.bounds)
    u, v, d = cam.project(corners[:, 0], corners[:, 1], 0.0)
    # 바닥은 근거리 클리핑을 위해 격자로 쪼개서 그림
    x0, y0, x1, y1 = lot.rect
    xs = np.linspace(x0, x1, 25)
    ys = np.linspace(y0, y1, 25)
    GX, GY = np.meshgrid(xs, ys)
    gu, gv, gd = cam.project(GX, GY, 0.0)
    for i in range(24, 0, -1):
        for j in range(24):
            q = [(i, j), (i, j + 1), (i - 1, j + 1), (i - 1, j)]
            if all(gd[a] > 0.3 for a in q):
                pygame.draw.polygon(surf, _fog(LOT, gd[q[0]]), [(gu[a], gv[a]) for a in q])
    for (ax, ay), (bx, by), col in lot.lines:
        n = max(2, int(math.hypot(bx - ax, by - ay) / 1.0))
        lx, ly = np.linspace(ax, bx, n), np.linspace(ay, by, n)
        lu, lv, ld = cam.project(lx, ly, 0.0)
        pts = [(lu[k], lv[k]) for k in range(n) if ld[k] > 0.3]
        if len(pts) > 1:
            pygame.draw.lines(surf, col, False, pts, 2)
    # 목표 주차칸 강조
    tgt = lot.spaces[lot.target]
    pu, pv, pd = cam.project(tgt.corners[:, 0], tgt.corners[:, 1], 0.02)
    if (pd > 0.3).all():
        pygame.draw.polygon(surf, (60, 200, 90), list(zip(pu, pv)), 3)


# ----------------------------------------------------------------------
def _box(cam, x, y, yaw, x0, x1, hw, z0, z1, pitch=0.0, roll=0.0):
    """차량 로컬 박스 [x0,x1]×[-hw,hw]×[z0,z1] -> 투영된 8개 꼭짓점."""
    c, s = math.cos(yaw), math.sin(yaw)
    lx = np.array([x0, x1, x1, x0, x0, x1, x1, x0])
    ly = np.array([-hw, -hw, hw, hw, -hw, -hw, hw, hw])
    lz = np.array([z0, z0, z0, z0, z1, z1, z1, z1], float)
    lz = lz - lx * pitch + ly * roll
    wx, wy = x + lx * c - ly * s, y + lx * s + ly * c
    return cam.project(wx, wy, lz), wx, wy, lz


BUILDING_COLORS = [(170, 160, 150), (140, 150, 165), (190, 180, 160), (120, 115, 110), (200, 200, 205)]
_sign_font = None


def _draw_sign(surf, cam, track, sg, dpt):
    """초록색 출구 안내 표지판: 기둥 2개 + 판 + 글자."""
    global _sign_font
    s, name, dist = sg
    d0 = -(track.rail_d + 1.0)
    for dd in (d0 + 1.8, d0 - 1.8):
        px, py, _ = track.to_world(s, dd)
        bu, bv, _ = cam.project(px, py, 0.0)
        tu, tv, _ = cam.project(px, py, 5.0)
        pygame.draw.line(surf, _fog(np.array([120, 120, 125]), dpt), (bu, bv), (tu, tv), max(1, int(cam.focal * 0.2 / dpt)))
    ds = np.array([d0 + 2.5, d0 - 2.5, d0 - 2.5, d0 + 2.5])
    zs = np.array([5.0, 5.0, 7.2, 7.2])
    px, py, _ = track.to_world(np.full(4, s), ds)
    u, v, d = cam.project(px, py, zs)
    if (d < 0.5).any():
        return
    pts = list(zip(u, v))
    pygame.draw.polygon(surf, _fog(np.array([20, 110, 60]), dpt), pts)
    pygame.draw.polygon(surf, (235, 235, 235), pts, 1)
    w, h = int(max(u) - min(u)), int(max(v) - min(v))
    if w > 30 and h > 10 and dpt < 250:
        if _sign_font is None:
            _sign_font = pygame.font.SysFont("arial", 40, bold=True)
        img = _sign_font.render(f"{name}  {dist if dist < 1000 else dist // 1000}{'m' if dist < 1000 else 'km'}",
                                True, (255, 255, 255))
        img = pygame.transform.smoothscale(img, (int(w * 0.9), int(h * 0.6)))
        surf.blit(img, img.get_rect(center=(sum(u) / 4, sum(v) / 4)))


_FACES = [  # (꼭짓점 인덱스, 로컬 법선, 밝기)
    ((0, 3, 7, 4), (-1, 0, 0), 0.75),   # 뒷면
    ((1, 2, 6, 5), (1, 0, 0), 0.85),    # 앞면
    ((0, 1, 5, 4), (0, -1, 0), 0.65),   # 오른쪽 면
    ((3, 2, 6, 7), (0, 1, 0), 0.65),    # 왼쪽 면
    ((4, 5, 6, 7), (0, 0, 1), 1.0),     # 윗면
]


def _draw_box(surf, cam, x, y, yaw, x0, x1, hw, z0, z1, color, dpt, rear_lights=None, pitch=0.0, roll=0.0):
    (u, v, d), wx, wy, wz = _box(cam, x, y, yaw, x0, x1, hw, z0, z1, pitch, roll)
    if (d < 0.3).any():
        return
    c, s = math.cos(yaw), math.sin(yaw)
    base = np.array(color, float)
    for idx, (nx, ny, nz), shade in _FACES:
        idx = list(idx)
        cx, cy, cz = wx[idx].mean(), wy[idx].mean(), wz[idx].mean()
        wnx, wny = nx * c - ny * s, nx * s + ny * c
        if (cam.x - cx) * wnx + (cam.y - cy) * wny + (cam.h - cz) * nz <= 0:
            continue
        pygame.draw.polygon(surf, _fog(base * shade, dpt), [(u[k], v[k]) for k in idx])
        if idx[0] == 0 and rear_lights is not None:
            # 후미등 (제동 시 밝아짐)
            for side in (-1, 1):
                ly = np.array([0.95, 0.55, 0.55, 0.95]) * hw * side
                lz = np.array([0.15, 0.15, 0.4, 0.4]) + z0 - (x0 - 0.02) * pitch + ly * roll
                px = x + (x0 - 0.02) * c - ly * s
                py = y + (x0 - 0.02) * s + ly * c
                pu, pv, _ = cam.project(px, py, lz)
                pygame.draw.polygon(surf, _fog(np.array(rear_lights), dpt), list(zip(pu, pv)))


def _draw_car(surf, cam, world, i, dpt, fx=None):
    L, Wd, Hh = world.length[i], world.width[i], world.height[i]
    x, y, yaw = world.x[i], world.y[i], world.yaw[i]
    braking = world.accel[i] < -1.0
    lights = (255, 40, 40) if braking else (150, 20, 20)
    col = world.color[i]
    p = fx.body_pitch() if fx else 0.0
    r = fx.body_roll() if fx else 0.0
    kind = world.kind[i]
    if kind == "truck":   # 화물칸 + 캐빈 (카메라에서 먼 것부터)
        cargo = (-L / 2, L / 2 - 2.6, Wd / 2, 0.5, Hh, col, dpt, lights, p, r)
        cab = (L / 2 - 2.4, L / 2, Wd / 2 - 0.05, 0.4, 3.0, (60, 60, 70), dpt, None, p, r)
        cam_front = (cam.x - x) * math.cos(yaw) + (cam.y - y) * math.sin(yaw) > L / 2 - 2.5
        for b in ((cargo, cab) if cam_front else (cab, cargo)):
            _draw_box(surf, cam, x, y, yaw, *b)
    elif kind == "bus":   # 차체 + 창문 띠
        _draw_box(surf, cam, x, y, yaw, -L / 2, L / 2, Wd / 2, 0.35, Hh - 1.2, col, dpt, lights, p, r)
        _draw_box(surf, cam, x, y, yaw, -L / 2 + 0.3, L / 2, Wd / 2, Hh - 1.2, Hh, (40, 50, 60), dpt,
                  None, p, r)
    else:
        _draw_box(surf, cam, x, y, yaw, -L / 2, L / 2, Wd / 2, 0.2, 0.85, col, dpt, lights, p, r)
        _draw_box(surf, cam, x, y, yaw, -L * 0.3, L * 0.18, Wd / 2 - 0.15, 0.85, Hh,
                  tuple(int(c * 0.6) for c in col), dpt, None, p, r)


def _draw_cockpit(surf, ego):
    W, H = surf.get_size()
    dash = (28, 30, 34)
    top = H * (0.83 if ego.kind == "car" else 0.88)
    pygame.draw.polygon(surf, dash, [(0, H), (0, top + H * 0.03), (W * 0.25, top), (W * 0.75, top),
                                     (W, top + H * 0.03), (W, H)])
    # 운전대 (조향각 x 조향비 만큼 회전)
    cx, cy, r = W * 0.5, H * 1.02, H * 0.2
    pygame.draw.circle(surf, (20, 20, 22), (int(cx), int(cy)), int(r), 14)
    ang = -ego.steer * 15.0
    for a in (ang - math.pi / 2, ang + math.pi / 2 - math.pi, ang + math.pi / 2):
        ex, ey = cx + r * math.cos(a), cy + r * math.sin(a)
        if ey < H:
            pygame.draw.line(surf, (20, 20, 22), (cx, cy), (ex, ey), 10)
    # A필러
    pygame.draw.polygon(surf, dash, [(0, 0), (W * 0.05, 0), (0, H * 0.5)])
    pygame.draw.polygon(surf, dash, [(W, 0), (W * 0.95, 0), (W, H * 0.5)])
