"""정비소 (CARLA 버전 메인 화면).

실제 CARLA 차를 카메라가 천천히 돌며 비추고, 그 위에 메뉴를 띄운다.
  - 출발 / 엔진 튜닝(파츠 장착) / 차량 선택 / 코스 선택 / 설정 / 종료
  - 오른쪽 성능 표: 최고출력, 최대토크, 최고속도, 0-100, 0-200 (튜닝 화면에서는 장착 전 미리보기)
파츠 내용과 계산은 sim/parts.py, 장착 상태는 config/garage.json (차종별, 장착 즉시 저장).
"""
import time

import pygame

from sim import i18n
from sim import parts as P
from sim.i18n import tr
from sim.vehicle import POWERTRAINS
from viz.menu import KINDS, LEVEL_NAME, LEVELS, PT_NAME

MAIN = [("drive", "Start driving", "Drive on the selected course"),
        ("tune", "Engine tuning", "Install intake, exhaust, turbo, ECU, motor and tire parts"),
        ("vehicle", "Choose vehicle", "Change body type and powertrain"),
        ("course", "Choose course", "Pick a course and difficulty"),
        ("settings", "Settings", "Traffic, detailed specs, camera, sound, language"),
        ("quit", "Quit", "Close the simulator")]
COURSE_DESC = {"easy": "Town04 highway, light traffic, clear",
               "normal": "Town04 highway, normal traffic, cloudy",
               "hard": "Town05 city, heavy traffic, wet sunset",
               "long": "30 km straight, 0-100/200/300 timing",
               "parking": "Parallel and perpendicular parking"}
TIER_COL = [(150, 155, 165), (90, 200, 120), (80, 160, 255), (255, 110, 70)]   # 순정/스트리트/스포츠/레이스
ACCENT = (255, 196, 0)
TEXT = (230, 235, 240)
DIM = (150, 160, 172)
GOOD, BAD = (110, 235, 140), (255, 110, 90)


def _effects(fx, spec, powertrain):
    """파츠 한 단계의 효과를 화면용 짧은 문구로."""
    out = []
    for key, label in (("power", "Power"), ("torque", "Torque"), ("motor_power", "Motor power"),
                       ("motor_torque", "Motor torque"), ("motor_rpm", "Motor rpm"), ("battery", "Battery"),
                       ("grip", "Grip")):
        if key in fx:
            out.append(tr("{what} +{p:.0f}%", what=tr(label), p=(fx[key] - 1) * 100))
    if "redline" in fx:
        out.append(tr("Redline +{rpm}", rpm=fx["redline"]))
    if "battery" in fx:
        out.append(tr("Weight +{kg:.0f} kg", kg=spec[powertrain]["battery_kwh"] * (fx["battery"] - 1) * P.KG_PER_KWH))
    return out


class Garage:
    def __init__(self, app):
        self.app = app
        self.open = False
        self.page, self.sel = "main", 0
        self.part_i, self.level_i, self.veh_row = 0, 0, 0
        self.course_i = LEVELS.index(app.cfg["level"])
        self.angle = 210.0
        self.msg, self.msg_until = "", 0.0
        self.f_title = i18n.font(40, bold=True)
        self.f_big = i18n.font(26)
        self.f = i18n.font(18)
        self.f_sm = i18n.font(15)

    # ------------------------------------------------------------------ 열기/닫기
    def show(self):
        """정비소로 들어온다: 차는 세워 두고(수동 모드) 카메라를 차 둘레로."""
        w = self.app.world
        self.open, self.page = True, "main"
        if w.mode != "manual":
            w.mode = "manual"
            w._apply_auto()
        w.enter_studio()                  # 갓길에 세우고 주변 가로등·표지판 등을 숨김 (카메라를 가리지 않게)
        w.set_view("showroom")

    def leave(self):
        """출발: 원래 시점(운전석 등)으로 돌아가 운전한다."""
        self.open = False
        self.app.world.leave_studio()     # 원래 자리로, 숨긴 구조물 다시 보이기
        self.app.view = self.app.view

    def update(self, dt):
        w = self.app.world
        if w.view != "showroom":          # 설정 메뉴에서 시점을 바꿨거나 차를 새로 만든 경우
            w.set_view("showroom")
        self.angle = (self.angle + 10.0 * dt) % 360
        w.orbit(self.angle)
        w.step(0.0, 1.0, 0.0)              # 차는 브레이크로 세워 두고 화면만 갱신

    # ------------------------------------------------------------------ 데이터
    def _kind_pt(self):
        e = self.app.world.ego
        return e.kind, e.powertrain

    def _parts(self):
        return P.applicable(self._kind_pt()[1])

    def _installed(self):
        return self.app.parts.get(self._kind_pt()[0], {})

    def _perf(self, installed=None):
        kind, pt = self._kind_pt()
        spec = P.apply_parts(self.app.specs[kind], self._installed() if installed is None else installed, pt)
        return P.performance(spec, kind, pt)

    def _say(self, msg):
        self.msg, self.msg_until = msg, time.time() + 2.5

    # ------------------------------------------------------------------ 키 입력
    def handle(self, ev):
        """KEYDOWN 처리. 'quit' 이면 종료 요청."""
        if ev.type != pygame.KEYDOWN:
            return None
        k = ev.key
        up, down = k == pygame.K_UP, k == pygame.K_DOWN
        left, right = k == pygame.K_LEFT, k == pygame.K_RIGHT
        enter = k in (pygame.K_RETURN, pygame.K_KP_ENTER)
        back = k in (pygame.K_ESCAPE, pygame.K_BACKSPACE)
        if self.page == "main":
            if up or down:
                self.sel = (self.sel + (1 if down else -1)) % len(MAIN)
            elif enter:
                return self._main(MAIN[self.sel][0])
            elif k == pygame.K_ESCAPE:
                self.leave()
        elif self.page == "tune":
            parts = self._parts()
            if up or down:
                self.part_i = (self.part_i + (1 if down else -1)) % len(parts)
                self.level_i = self._installed().get(parts[self.part_i][0], 0)
            elif left or right:
                self.level_i = max(0, min(len(P.LEVELS) - 1, self.level_i + (1 if right else -1)))
            elif enter:
                self._install(parts[self.part_i])
            elif back:
                self.page = "main"
        elif self.page == "vehicle":
            w, (kind, pt) = self.app.world, self._kind_pt()
            if up or down:
                self.veh_row = 1 - self.veh_row
            elif left or right:
                d = 1 if right else -1
                if self.veh_row == 0:
                    w.set_vehicle(kind=KINDS[(KINDS.index(kind) + d) % len(KINDS)])
                else:
                    w.set_vehicle(powertrain=POWERTRAINS[(POWERTRAINS.index(pt) + d) % len(POWERTRAINS)])
            elif enter or back:
                self.page = "main"
        elif self.page == "course":
            if up or down:
                self.course_i = (self.course_i + (1 if down else -1)) % len(LEVELS)
            elif enter:
                if LEVELS[self.course_i] == self.app.cfg["level"]:
                    self.leave()
                else:                            # 다른 코스: 새로 불러온 뒤 바로 운전 화면
                    self.app.cfg["level"] = LEVELS[self.course_i]
                    self.open = False
                    self.app.rebuild()
            elif back:
                self.page = "main"
        return None

    def _main(self, key):
        if key == "drive":
            self.leave()
        elif key == "tune":
            self.page, self.part_i = "tune", 0
            self.level_i = self._installed().get(self._parts()[0][0], 0)
        elif key == "vehicle":
            self.page, self.veh_row = "vehicle", 0
        elif key == "course":
            self.page, self.course_i = "course", LEVELS.index(self.app.cfg["level"])
        elif key == "settings":
            self.app.menu.open = True
        elif key == "quit":
            return "quit"
        return None

    def _install(self, part):
        kind = self._kind_pt()[0]
        mine = self.app.parts.setdefault(kind, {})
        if self.level_i:
            mine[part[0]] = self.level_i
        else:
            mine.pop(part[0], None)
        P.save_parts(self.app.parts)
        self.app.world.set_vehicle()         # 같은 자리에서 새 제원 적용
        self._say(tr("Installed: {part} ({level})", part=tr(part[1]), level=tr(P.LEVELS[self.level_i])))

    # ------------------------------------------------------------------ 그리기
    def _shade(self, surf, rect, alpha=200, col=(8, 11, 16)):
        sh = pygame.Surface(rect[2:], pygame.SRCALPHA)
        sh.fill((*col, alpha))
        surf.blit(sh, rect[:2])

    def _text(self, surf, s, pos, col=TEXT, font=None, right=False):
        img = (font or self.f).render(s, True, col)
        surf.blit(img, (pos[0] - img.get_width(), pos[1]) if right else pos)
        return img.get_width()

    def draw(self, surf):
        W, H = surf.get_size()
        w = self.app.world
        if w.frame is not None:
            surf.blit(pygame.surfarray.make_surface(w.frame.swapaxes(0, 1)), (0, 0))
        else:
            surf.fill((20, 22, 26))
        LW, RW = 360, 390
        self._shade(surf, (0, 0, LW, H), 215)
        self._shade(surf, (W - RW, 0, RW, H), 215)
        # 위 막대: 제목 + 공사장 줄무늬
        self._shade(surf, (0, 0, W, 70), 235, (14, 14, 16))
        for x in range(-40, W + 40, 28):
            pygame.draw.polygon(surf, ACCENT, [(x, 78), (x + 14, 78), (x + 28, 70), (x + 14, 70)])
        pygame.draw.line(surf, (30, 30, 30), (0, 78), (W, 78), 1)
        self._text(surf, tr("GARAGE"), (24, 12), ACCENT, self.f_title)
        kind, pt = self._kind_pt()
        self._text(surf, f"{tr(w.ego.spec['name'])}  ·  {tr(PT_NAME[pt])}", (W - 24, 14), TEXT, self.f_big, right=True)
        self._text(surf, tr("Course: {name}", name=tr(LEVEL_NAME[self.app.cfg["level"]])), (W - 24, 46), DIM,
                   self.f_sm, right=True)
        getattr(self, "_page_" + self.page)(surf, W, H, LW)
        self._stats(surf, W, H, RW)
        # 아래 안내
        self._shade(surf, (0, H - 34, W, 34), 235, (14, 14, 16))
        hints = {"main": "Up/Down select   Enter OK   Esc back to driving",
                 "tune": "Up/Down part   Left/Right level   Enter install   Esc back",
                 "vehicle": "Up/Down row   Left/Right change   Esc back",
                 "course": "Up/Down course   Enter go   Esc back"}
        self._text(surf, tr(hints[self.page]), (24, H - 28), DIM, self.f_sm)
        if self.msg and time.time() < self.msg_until:
            img = self.f_big.render(self.msg, True, GOOD)
            x = (W - img.get_width()) // 2
            self._shade(surf, (x - 16, 96, img.get_width() + 32, 44), 225)
            surf.blit(img, (x, 104))

    def _row(self, surf, y, h, on, LW):
        if on:
            self._shade(surf, (0, y, LW, h), 160, (60, 70, 85))
            pygame.draw.rect(surf, ACCENT, (0, y, 6, h))

    def _page_main(self, surf, W, H, LW):
        y = 110
        for i, (key, label, desc) in enumerate(MAIN):
            on = i == self.sel
            h = 72 if on else 50
            self._row(surf, y, h, on, LW)
            self._text(surf, tr(label), (28, y + 10), ACCENT if on and key == "drive" else TEXT, self.f_big)
            if on:
                self._text(surf, tr(desc), (30, y + 44), DIM, self.f_sm)
            y += h + 4

    def _page_tune(self, surf, W, H, LW):
        self._text(surf, tr("Engine tuning"), (24, 96), ACCENT, self.f_big)
        parts, inst = self._parts(), self._installed()
        y = 140
        for i, p in enumerate(parts):
            on = i == self.part_i
            self._row(surf, y, 34, on, LW)
            self._text(surf, tr(p[1]), (24, y + 6), TEXT if on else (205, 210, 216))
            lv = inst.get(p[0], 0)
            for j in range(3):                   # 장착 단계 (칸 3개)
                pygame.draw.rect(surf, TIER_COL[lv] if j < lv else (60, 64, 72), (LW - 76 + j * 20, y + 11, 15, 12))
            y += 36
        # 단계 카드 (화면 가운데 아래)
        p = parts[self.part_i]
        kind, pt = self._kind_pt()
        gap = 6
        cw = (W - LW - 390 - 16 - 3 * gap) // 4      # 왼쪽·오른쪽 판 사이에 카드 4장
        x0 = LW + 8
        y0 = H - 34 - 150
        self._text(surf, tr(p[1]), (x0, y0 - 30), TEXT, self.f)
        for lv in range(len(P.LEVELS)):
            x = x0 + lv * (cw + gap)
            on, mounted = lv == self.level_i, lv == inst.get(p[0], 0)
            self._shade(surf, (x, y0, cw, 140), 235 if on else 200, (30, 34, 42) if on else (14, 16, 20))
            pygame.draw.rect(surf, TIER_COL[lv], (x, y0, cw, 5))
            if on:
                pygame.draw.rect(surf, ACCENT, (x, y0, cw, 140), 2)
            self._text(surf, tr(P.LEVELS[lv]), (x + 10, y0 + 12), TIER_COL[lv], self.f)
            for k, line in enumerate(_effects(p[3][lv], self.app.specs[kind], pt)[:4] or [tr("Factory parts")]):
                self._text(surf, line, (x + 10, y0 + 40 + 19 * k), TEXT, self.f_sm)
            if mounted:
                self._text(surf, tr("Installed"), (x + 10, y0 + 114), GOOD, self.f_sm)
            elif on:
                self._text(surf, tr("Enter: install"), (x + 10, y0 + 114), ACCENT, self.f_sm)

    def _page_vehicle(self, surf, W, H, LW):
        self._text(surf, tr("Choose vehicle"), (24, 96), ACCENT, self.f_big)
        kind, pt = self._kind_pt()
        rows = [("Vehicle type", tr(self.app.specs[kind]["name"])), ("Powertrain", tr(PT_NAME[pt]))]
        y = 145
        for i, (label, val) in enumerate(rows):
            on = i == self.veh_row
            self._row(surf, y, 66, on, LW)
            self._text(surf, tr(label), (24, y + 6), DIM, self.f_sm)
            self._text(surf, f"<  {val}  >" if on else val, (24, y + 28), ACCENT if on else TEXT, self.f)
            y += 72
        y += 20
        for s in ("Parts are saved per vehicle type.", "Engine parts: engine cars and hybrids.",
                  "Electric parts: EVs and hybrids."):
            self._text(surf, tr(s), (24, y), DIM, self.f_sm)
            y += 22

    def _page_course(self, surf, W, H, LW):
        self._text(surf, tr("Choose course"), (24, 96), ACCENT, self.f_big)
        y = 140
        for i, key in enumerate(LEVELS):
            on = i == self.course_i
            self._row(surf, y, 60, on, LW)
            cur = tr("  (current)") if key == self.app.cfg["level"] else ""
            self._text(surf, tr(LEVEL_NAME[key]) + cur, (24, y + 6), TEXT if on else (205, 210, 216), self.f)
            self._text(surf, tr(COURSE_DESC[key]), (24, y + 33), DIM, self.f_sm)
            y += 64

    def _stats(self, surf, W, H, RW):
        """오른쪽 성능 표. 튜닝 화면에서 다른 단계를 고르면 바뀔 값을 초록/빨강으로 미리 보여 준다."""
        x = W - RW + 22
        kind, pt = self._kind_pt()
        stock = P.performance(self.app.specs[kind], kind, pt)
        cur = self._perf()
        new = cur
        if self.page == "tune":
            p = self._parts()[self.part_i]
            cand = dict(self._installed())
            cand[p[0]] = self.level_i
            new = self._perf(cand)
        self._text(surf, tr("PERFORMANCE"), (x, 96), ACCENT, self.f_big)
        n = len([1 for p in self._parts() if self._installed().get(p[0], 0)])
        look = P.tier(self._installed(), pt)
        self._text(surf, tr("{n} tuning parts installed", n=n) + "   " + tr("Look: {tier}", tier=tr(P.LEVELS[look])),
                   (x, 130), TIER_COL[look] if look else DIM, self.f_sm)

        def t_txt(t):
            return "-" if t is None else f"{t:.1f} s"

        rows = [  # (라벨, 지금 값, 바뀔 값, 표시, 막대 기준, 클수록 좋음)
            ("Max power", cur["power_hp"], new["power_hp"], lambda v: f"{v:.0f} hp", stock["power_hp"] * 3.2, True),
            ("Max torque", cur["torque_nm"], new["torque_nm"], lambda v: f"{v:.0f} Nm", stock["torque_nm"] * 2.6, True),
            ("Top speed (est.)", cur["top_kmh"], new["top_kmh"],
             lambda v: f"{v:.0f} km/h" + (tr(" (limiter)") if cur["limited"] else ""), 400.0, True),
            ("0-100 km/h (est.)", cur["t100"], new["t100"], t_txt, stock["t100"], False),
            ("0-200 km/h (est.)", cur["t200"], new["t200"], t_txt, stock["t200"], False),
        ]
        y = 165
        bw = RW - 44
        for label, a, b, fmt, scale, more_better in rows:
            self._text(surf, tr(label), (x, y), DIM, self.f_sm)
            self._text(surf, fmt(a), (x + bw, y - 2), TEXT, self.f, right=True)
            if a is not None and b is not None and abs(b - a) > 1e-6:
                better = (b > a) == more_better
                d = b - a
                self._text(surf, ("+" if d > 0 else "") + (f"{d:.1f}" if not more_better else f"{d:.0f}"),
                           (x + bw, y + 20), GOOD if better else BAD, self.f_sm, right=True)

            def frac(v):
                if v is None or not scale:
                    return 0.0
                return min(1.0, v / scale if more_better else 0.5 * scale / v)

            fa, fb = frac(a), frac(b)
            pygame.draw.rect(surf, (45, 50, 58), (x, y + 24, bw - 70, 8))
            pygame.draw.rect(surf, (225, 230, 236), (x, y + 24, int((bw - 70) * min(fa, fb)), 8))
            if fb != fa:
                lo, hi = sorted((fa, fb))
                pygame.draw.rect(surf, GOOD if fb > fa else BAD,
                                 (x + int((bw - 70) * lo), y + 24, max(2, int((bw - 70) * (hi - lo))), 8))
            y += 52
        y += 6
        self._text(surf, tr("Weight"), (x, y), DIM, self.f_sm)
        self._text(surf, f"{new['mass']:.0f} kg", (x + bw, y - 2), TEXT, self.f, right=True)
        y += 28
        self._text(surf, tr("Weight per hp"), (x, y), DIM, self.f_sm)
        self._text(surf, f"{new['kg_per_hp']:.1f} kg/hp", (x + bw, y - 2), TEXT, self.f, right=True)
        y += 34
        for s in ("Estimated with the simple car model.", "Measure real times on the 30 km straight."):
            self._text(surf, tr(s), (x, y), (120, 128, 140), self.f_sm)
            y += 20
