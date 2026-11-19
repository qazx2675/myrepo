"""위에서 내려다본 2D 뷰 (진행방향이 위쪽). 차간 거리/차선 변경/주차 확인용."""
import math

import numpy as np
import pygame


def draw_topdown(surf, world, scale=None):
    W, H = surf.get_size()
    e = world.ego
    lot = getattr(world, "lot", None)
    scale = scale or (14.0 if lot is not None else 6.0)
    cx, cy = W / 2, H * (0.55 if lot is not None else 0.65)
    c, s = math.cos(e.yaw), math.sin(e.yaw)

    def to_screen(x, y):
        dx, dy = x - e.x, y - e.y
        f = dx * c + dy * s
        left = -dx * s + dy * c
        return cx - left * scale, cy - f * scale

    surf.fill((80, 132, 58))
    if lot is not None:
        x0, y0, x1, y1 = lot.rect
        pygame.draw.polygon(surf, (105, 107, 110), [to_screen(x, y) for x, y in ((x0, y0), (x1, y0), (x1, y1), (x0, y1))])
        for a, b, col in lot.lines:
            pygame.draw.line(surf, col, to_screen(*a), to_screen(*b), 2)
        tgt = lot.spaces[lot.target]
        pygame.draw.polygon(surf, (60, 200, 90), [to_screen(x, y) for x, y in tgt.corners], 3)
    else:
        _draw_roads(surf, world, to_screen, scale, H)

    # 차량
    for i in range(world.n):
        u, v = to_screen(world.x[i], world.y[i])
        if not (-80 < u < W + 80 and -80 < v < H + 80):
            continue
        L, Wd, yaw = world.length[i], world.width[i], world.yaw[i]
        cy_, sy_ = math.cos(yaw), math.sin(yaw)
        pts = [to_screen(world.x[i] + lx * cy_ - ly * sy_, world.y[i] + lx * sy_ + ly * cy_)
               for lx, ly in ((L / 2, Wd / 2), (L / 2, -Wd / 2), (-L / 2, -Wd / 2), (-L / 2, Wd / 2))]
        pygame.draw.polygon(surf, world.color[i], pts)
        pygame.draw.polygon(surf, (20, 20, 20), pts, 1)
        pygame.draw.line(surf, (230, 230, 255), pts[0], pts[1], 2)          # 앞쪽 표시
        if world.accel[i] < -1.0:   # 제동등
            pygame.draw.line(surf, (255, 40, 40), pts[2], pts[3], 3)

    if lot is None:
        j = int(world.leader[0])
        if j >= 0 and world.gap[0] < 150:
            pygame.draw.line(surf, (255, 230, 0), to_screen(e.x, e.y), to_screen(world.x[j], world.y[j]), 2)


def _strip(surf, road, ss, d0, d1, color, to_screen):
    x0, y0, _ = road.to_world(ss, d0)
    x1, y1, _ = road.to_world(ss, d1)
    a = list(zip(*to_screen(x0, y0)))
    b = list(zip(*to_screen(x1, y1)))
    pygame.draw.polygon(surf, color, a + b[::-1])


def _draw_roads(surf, world, to_screen, scale, H):
    t = world.track
    s_main = world.ego_s_main
    span_back, span_fwd = H * 0.4 / scale + 30, H * 0.7 / scale + 30
    ss = s_main + np.arange(-span_back, span_fwd, 2.0)
    hw = t.half_width
    for r in t.ramps:
        if (s_main - r.s0) % t.L < r.L + 100 or (r.s0 - s_main) % t.L < span_fwd + 50:
            rs = np.arange(0.0, r.L, 2.0)
            _strip(surf, r, rs, r.half_width + 1.0, -r.half_width - 1.0, (125, 125, 122), to_screen)
            _strip(surf, r, rs, r.half_width, -r.half_width, (95, 97, 100), to_screen)
            for side in (1, -1):
                q = rs[rs > r.taper + 20] if side > 0 else rs
                if len(q) > 1:
                    rx, ry, _ = r.to_world(q, side * r.rail_d)
                    pygame.draw.lines(surf, (200, 200, 205), False, list(zip(*to_screen(rx, ry))), 3)
    _strip(surf, t, ss, hw + t.shoulder, -hw - t.shoulder, (125, 125, 122), to_screen)
    _strip(surf, t, ss, hw, -hw, (95, 97, 100), to_screen)
    _strip(surf, t, ss, hw - 0.15, hw - 0.35, (230, 190, 40), to_screen)
    _strip(surf, t, ss, -hw + 0.35, -hw + 0.15, (235, 235, 235), to_screen)
    gap = np.array([t.in_exit_zone(v, 60.0) for v in ss])
    for side in (1, -1):
        rx, ry, _ = t.to_world(ss, side * t.rail_d)
        u, v = to_screen(rx, ry)
        for k in range(len(ss) - 1):
            if side > 0 or not (gap[k] or gap[k + 1]):
                pygame.draw.line(surf, (200, 200, 205), (u[k], v[k]), (u[k + 1], v[k + 1]), 3)
    first = math.floor((s_main - span_back) / 20.0) * 20.0
    starts = first + 20.0 * np.arange(int((span_back + span_fwd) / 20) + 2)
    for k in range(1, t.n_lanes):
        dk = hw - k * t.lane_width
        x0, y0, _ = t.to_world(starts, dk)
        x1, y1, _ = t.to_world(starts + 8, dk)
        u0, v0 = to_screen(x0, y0)
        u1, v1 = to_screen(x1, y1)
        for j in range(len(starts)):
            pygame.draw.line(surf, (235, 235, 235), (u0[j], v0[j]), (u1[j], v1[j]), max(1, int(0.15 * scale)))
