"""HUD: 속도계/회전계/기어, 제한속도, 미니맵(경로), 내비 안내, 차간거리, 배터리, 제로백 기록."""
import math

import numpy as np
import pygame

HELP = [
    "W/S  accel / brake    A/D  steer",
    "G    drive <-> reverse (stopped)",
    "Z/X  gear down / up (manual)",
    "M    mode MANUAL > ACC > AUTO",
    "Q/E  lane change (AUTO)  [ ] ACC speed",
    "N    destination  (route guidance)",
    "T    vehicle car/bus/truck",
    "P    powertrain auto/manual/ev/hybrid",
    "V    view cockpit/chase/top",
    "F1-F4 easy/normal/hard/long   F5 parking",
    "C    camera FX on/off   O sound on/off",
    "K    external controller   Bksp reset",
    "H    help   Space pause   Esc quit",
]
PT_LABEL = {"auto": "ICE-AT", "manual": "ICE-MT", "ev": "EV", "hybrid": "HYBRID"}


class Hud:
    def __init__(self, world):
        self.f_big = pygame.font.SysFont("arial", 44, bold=True)
        self.f_mid = pygame.font.SysFont("arial", 22, bold=True)
        self.f_sm = pygame.font.SysFont("arial", 16)
        self.f_mono = pygame.font.SysFont("consolas,dejavusansmono,monospace", 16)
        self.show_help = True
        self.set_world(world)

    def set_world(self, world):
        if hasattr(world, "track"):
            t = world.track
            step = max(1, int(8.0 / t.ds))
            self.map_main = (t.x[::step], t.y[::step])
            self.map_ramps = [(r.x[::10], r.y[::10]) for r in t.ramps]

    def text(self, surf, txt, font, pos, color=(255, 255, 255), center=False, shadow=True):
        img = font.render(txt, True, color)
        r = img.get_rect()
        if center:
            r.center = pos
        else:
            r.topleft = pos
        if shadow:
            surf.blit(font.render(txt, True, (0, 0, 0)), r.move(1, 1))
        surf.blit(img, r)

    def lines_box(self, surf, lines, x=10, y=10, w=330):
        box = pygame.Surface((w, 22 * len(lines) + 12), pygame.SRCALPHA)
        box.fill((10, 12, 15, 160))
        surf.blit(box, (x, y))
        for k, txt in enumerate(lines):
            self.text(surf, txt, self.f_mono, (x + 8, y + 6 + 22 * k), shadow=False)

    def draw(self, surf, world, fps, paused, view):
        W, H = surf.get_size()
        self._speedometer(surf, world.ego, 120, H - 115)
        if hasattr(world, "track"):
            obs = world.ego_observation()
            self._right_panel(surf, world, obs)
            self._nav_banner(surf, world)
            self._info(surf, world, obs, fps, paused, view)
            self._accel_timer(surf, world)
        else:
            world.draw_hud(self, surf, fps, paused, view)
        if self.show_help:
            self._help(surf)

    # ------------------------------------------------------------------
    def _speedometer(self, surf, e, cx, cy, r=95):
        kmh = abs(e.vx) * 3.6
        vmax = 320 if (e.kind == "car" and e.spec["power_hp"] > 300) else 180
        pygame.draw.circle(surf, (20, 22, 26), (cx, cy), r + 6)
        pygame.draw.circle(surf, (70, 75, 85), (cx, cy), r + 6, 2)
        a0, a1 = math.radians(225), math.radians(-45)
        step = 20 if vmax > 200 else 10
        for k in range(0, vmax + 1, step):
            a = a0 + (a1 - a0) * k / vmax
            r1 = r - (14 if k % (step * 2) == 0 else 7)
            pygame.draw.line(surf, (220, 220, 220), (cx + r * math.cos(a), cy - r * math.sin(a)),
                             (cx + r1 * math.cos(a), cy - r1 * math.sin(a)), 2)
            if k % (step * 4) == 0:
                self.text(surf, str(k), self.f_sm, (cx + (r - 28) * math.cos(a), cy - (r - 28) * math.sin(a)),
                          (200, 200, 200), center=True, shadow=False)
        a = a0 + (a1 - a0) * min(kmh, vmax) / vmax
        pygame.draw.line(surf, (240, 60, 40), (cx, cy), (cx + (r - 8) * math.cos(a), cy - (r - 8) * math.sin(a)), 4)
        pygame.draw.circle(surf, (240, 60, 40), (cx, cy), 6)
        self.text(surf, f"{kmh:3.0f}", self.f_big, (cx, cy + 38), center=True, shadow=False)
        self.text(surf, "km/h", self.f_sm, (cx, cy + 66), (180, 180, 180), center=True, shadow=False)

        # 회전계(또는 모터 출력) 막대 + 기어 + 상태
        bx, by = cx + r + 20, cy - 60
        electric = e.powertrain == "ev" or (e.powertrain == "hybrid" and not e.engine_on)
        if electric:
            cap = e.spec["ev"]["motor_power_kw"] if e.powertrain == "ev" else e.spec["hybrid"]["motor_hp"] * 0.7457
            frac, val = min(1.0, abs(e.power_kw) / cap), f"{e.power_kw:.0f} kW"
            col = (80, 200, 255) if e.power_kw >= 0 else (80, 230, 120)
        else:
            frac, val = min(1.0, e.rpm / e.eng["redline_rpm"]), f"{e.rpm:.0f} rpm"
            col = (240, 60, 40) if frac > 0.9 else (240, 200, 60)
        pygame.draw.rect(surf, (20, 22, 26), (bx, by, 22, 120))
        pygame.draw.rect(surf, col, (bx + 3, by + 3 + 114 * (1 - frac), 16, 114 * frac))
        self.text(surf, val, self.f_sm, (bx + 28, by))
        if e.reverse:
            gear = "R"
        elif e.powertrain == "ev":
            gear = "D"
        elif e.powertrain == "manual" and not e.auto_shift:
            gear = f"M{e.gear}"
        else:
            gear = f"D{e.gear}"
        self.text(surf, gear, self.f_big, (bx + 28, by + 18))
        self.text(surf, f"{e.spec['name']} {PT_LABEL[e.powertrain]}", self.f_sm, (bx + 28, by + 70))
        if e.powertrain in ("ev", "hybrid"):
            extra = "  ENG" if e.powertrain == "hybrid" and e.engine_on else ""
            self.text(surf, f"BATT {e.soc * 100:4.1f}%{extra}", self.f_sm, (bx + 28, by + 90), (120, 230, 140))
        warn = [w for w, on in (("TCS", e.wheelspin), ("ABS", e.abs_active), ("SLIP", e.sliding),
                                ("OFFROAD", e.offroad)) if on]
        if warn:
            self.text(surf, " ".join(warn), self.f_mid, (bx + 28, by + 110), (255, 170, 40))

    def _limit_sign(self, surf, limit, cx, cy, r=26):
        pygame.draw.circle(surf, (255, 255, 255), (cx, cy), r)
        pygame.draw.circle(surf, (210, 30, 30), (cx, cy), r, 6)
        self.text(surf, f"{limit:.0f}", self.f_mid, (cx, cy), (0, 0, 0), center=True, shadow=False)

    def _right_panel(self, surf, world, obs):
        W, H = surf.get_size()
        mw, mh = 230, 260
        mx, my = W - mw - 15, H - mh - 15
        panel = pygame.Surface((mw, mh), pygame.SRCALPHA)
        panel.fill((15, 18, 22, 190))
        self._minimap(panel, world)
        surf.blit(panel, (mx, my))
        pygame.draw.rect(surf, (90, 95, 105), (mx, my, mw, mh), 1)
        kmh = obs["v"] * 3.6
        lim = obs["speed_limit_kmh"]
        bx, by = mx - 70, my + 10
        pygame.draw.rect(surf, (25, 28, 32), (bx, by, 60, 50), border_radius=6)
        col = (255, 90, 80) if kmh > lim + 3 else (255, 255, 255)
        self.text(surf, f"{kmh:.0f}", self.f_mid, (bx + 30, by + 18), col, center=True, shadow=False)
        self.text(surf, "km/h", self.f_sm, (bx + 30, by + 38), (180, 180, 180), center=True, shadow=False)
        self._limit_sign(surf, lim, bx + 30, by + 92)

    def _minimap(self, panel, world, scale=0.14):
        mw, mh = panel.get_size()
        e = world.ego
        c, s = math.cos(e.yaw), math.sin(e.yaw)
        cx, cy = mw / 2, mh * 0.7

        def tr(x, y):
            dx, dy = x - e.x, y - e.y
            return cx - (-dx * s + dy * c) * scale, cy - (dx * c + dy * s) * scale

        u, v = tr(*self.map_main)
        pts = list(zip(u, v))
        pygame.draw.lines(panel, (110, 110, 115), True, pts, 9)
        dest = world.nav.dest
        for k, (rx, ry) in enumerate(self.map_ramps):
            ru, rv = tr(rx, ry)
            pygame.draw.lines(panel, (60, 200, 90) if k == dest else (110, 110, 115), False, list(zip(ru, rv)), 5)
            if k == dest:
                pygame.draw.circle(panel, (60, 200, 90), (int(ru[-1]), int(rv[-1])), 6)
        # 경로 강조 (내 위치 -> 목적지)
        if dest is not None and e.road is None:
            r = world.track.ramps[dest]
            de = max(world.nav.dist_to_exit(e), 0.0)
            px, py, _ = world.track.to_world(e.s + np.linspace(0, min(de, 3000.0), 60), 0.0)
            pu, pv = tr(px, py)
            pygame.draw.lines(panel, (250, 150, 40), False, list(zip(pu, pv)), 5)
            if de < 3000:
                ru, rv = tr(r.x[::10], r.y[::10])
                pygame.draw.lines(panel, (250, 150, 40), False, list(zip(ru, rv)), 5)
        pygame.draw.lines(panel, (250, 200, 60), True, pts, 2)
        for i in range(1, world.n):
            pu, pv = tr(world.x[i], world.y[i])
            if 0 <= pu < mw and 0 <= pv < mh:
                pygame.draw.circle(panel, (255, 255, 255) if world.kind[i] == "car" else (120, 200, 255),
                                   (int(pu), int(pv)), 2)
        pygame.draw.polygon(panel, (60, 160, 255), [(cx, cy - 9), (cx - 6, cy + 6), (cx + 6, cy + 6)])

    def _nav_banner(self, surf, world):
        W, _ = surf.get_size()
        g = world.nav.guidance(world.ego)
        bw, bh = 560, 56
        x0 = (W - bw) // 2
        box = pygame.Surface((bw, bh), pygame.SRCALPHA)
        box.fill((20, 70, 40, 210) if g["active"] or g["arrived"] else (20, 22, 26, 150))
        surf.blit(box, (x0, 8))
        ax, ay = x0 + 30, 36
        if g["arrow"] in ("up", "right_lane", "exit"):
            pygame.draw.polygon(surf, (255, 255, 255), [(ax, ay - 16), (ax - 11, ay - 2), (ax - 4, ay - 2),
                                                        (ax - 4, ay + 16), (ax + 4, ay + 16), (ax + 4, ay - 2),
                                                        (ax + 11, ay - 2)])
            if g["arrow"] != "up":
                pygame.draw.line(surf, (255, 210, 60), (ax + 2, ay), (ax + 18, ay - 12), 5)
        elif g["arrow"] == "flag":
            pygame.draw.line(surf, (255, 255, 255), (ax - 6, ay + 16), (ax - 6, ay - 16), 3)
            pygame.draw.polygon(surf, (230, 60, 60), [(ax - 6, ay - 16), (ax + 14, ay - 9), (ax - 6, ay - 2)])
        self.text(surf, g["instruction"], self.f_mid, (x0 + 56, 14))
        if g["active"] and g["distance"] is not None and not g["arrived"]:
            self.text(surf, f"to {g['destination']}: {g['distance'] / 1000:.2f} km", self.f_sm,
                      (x0 + 56, 40), (200, 230, 200))

    def _info(self, surf, world, obs, fps, paused, view):
        mode = world.mode_label or world.ego_mode.upper()
        if world.ego_mode != "manual" and world.ego_set_speed is not None and not world.mode_label:
            mode += f" {world.ego_set_speed * 3.6:.0f}km/h"
        lines = [f"MODE  {mode}  [{world.level['label']}]"]
        lane_txt = "RAMP" if obs["on_ramp"] else f"LANE  {obs['lane'] + 1} / {world.track.n_lanes}"
        if world.ego_mode == "auto" and not obs["on_ramp"]:
            lane_txt += f"  (target {world.ego_target_lane + 1})"
        lines.append(lane_txt)
        if obs["lead_gap"] is not None and obs["lead_gap"] < 250:
            thw = obs["lead_gap"] / max(obs["v"], 0.1)
            lines.append(f"GAP   {obs['lead_gap']:6.1f} m   THW {min(thw, 99):4.1f} s")
            lines.append(f"LEAD  {obs['lead_v'] * 3.6:6.1f} km/h")
        else:
            lines.append("GAP   ---")
        lines.append(f"COLLISION {world.collisions}   RAIL {world.rail_hits}")
        lines.append(f"TRAFFIC {world.n - 1}   ODO {world.odometer / 1000:.1f} km")
        lines.append(f"VIEW {view}   {fps:4.0f} FPS" + ("   [PAUSED]" if paused else ""))
        self.lines_box(surf, lines)

    def _accel_timer(self, surf, world):
        at = world.accel_timer
        if not at.results and not at.best:
            return
        x0, y0 = 10, 175
        box = pygame.Surface((240, 92), pygame.SRCALPHA)
        box.fill((10, 12, 15, 160))
        surf.blit(box, (x0, y0))
        self.text(surf, "0-100 / 200 / 300 km/h", self.f_sm, (x0 + 8, y0 + 4), (255, 210, 60), shadow=False)
        for k, m in enumerate(at.MARKS):
            cur, best = at.results.get(m), at.best.get(m)
            txt = f"{m:3d}: {cur:5.2f}s" if cur is not None else f"{m:3d}:   --  "
            if best is not None:
                txt += f"  best {best:5.2f}"
            self.text(surf, txt, self.f_mono, (x0 + 8, y0 + 24 + 20 * k), shadow=False)

    def _help(self, surf):
        W, _ = surf.get_size()
        box = pygame.Surface((340, 20 * len(HELP) + 14), pygame.SRCALPHA)
        box.fill((10, 12, 15, 150))
        surf.blit(box, (W - 355, 72))
        for k, txt in enumerate(HELP):
            self.text(surf, txt, self.f_mono, (W - 345, 79 + 20 * k), (220, 220, 220), shadow=False)
