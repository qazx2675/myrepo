"""정비소 (CARLA 버전 메인 화면).

실제 CARLA 차를 카메라가 천천히 돌며 비추고, 그 위에 메뉴를 띄운다.
  - 출발 / 엔진 튜닝(파츠 장착) / 차량 선택 / 코스 선택 / 설정 / 종료
  - 세부 조정: 출력·토크·레드라인·종감속비·기어비·접지·브레이크를 0.5% 단위로 (Shift = 5%)
  - 다이노 테스트: CARLA 에 들어가는 토크 곡선 그대로 출력/토크 그래프. W 로 직접 스로틀을 밟아 엔진을 돌리고,
    Enter/Space = 풀스로틀 측정(최고 회전까지 자동으로 끝까지). 풀스로틀 구간을 측정 곡선으로 그린다.
  - 오른쪽 성능 표: 최고출력, 최대토크, 최고속도, 0-100, 0-200 (튜닝 화면에서는 장착 전 미리보기)
파츠 내용과 계산은 sim/parts.py, 장착 상태는 config/garage.json (차종별, 장착 즉시 저장).
"""
import copy
import math
import time

import pygame

from carla_sim.physics import curve_for

from sim import i18n
from sim import parts as P
from sim.i18n import tr
from sim.vehicle import POWERTRAINS
from viz.menu import KINDS, LEVEL_NAME, LEVELS, PT_NAME

MAIN = [("drive", "Start driving", "Drive on the selected course"),
        ("tune", "Engine tuning", "Install intake, exhaust, turbo, ECU, motor and tire parts"),
        ("fine", "Fine tuning", "Adjust power, gearing, grip and brakes in fine steps"),
        ("dyno", "Dyno test", "Power and torque curves, run the engine on the dyno"),
        ("vehicle", "Choose vehicle", "Change body type and powertrain"),
        ("course", "Choose course", "Pick a course and difficulty"),
        ("settings", "Settings", "Traffic, detailed specs, camera, sound, language"),
        ("quit", "Quit", "Close the simulator")]
COURSE_DESC = {"easy": "Town04 highway, light traffic, clear",
               "normal": "Town04 highway, normal traffic, cloudy",
               "hard": "Town05 city, heavy traffic, wet sunset",
               "long": "30 km straight, 0-100/200/300 timing",
               "parking": "Parallel and perpendicular parking"}
TUNE_LABEL = {"power": "Engine power", "torque": "Engine torque", "redline": "Redline", "motor_power": "Motor power",
              "motor_torque": "Motor torque", "motor_rpm": "Motor max rpm", "final": "Final drive", "grip": "Tire grip",
              "brake": "Brake force"}
DYNO_SWEEP = 4.0                                      # 다이노: 풀스로틀로 공회전 → 최고 회전까지 약 몇 초
DYNO_FALL = 1.5                                       # 다이노: 페달을 떼면 최고 회전 → 공회전까지 약 몇 초
POWER_COL, TORQUE_COL = (255, 150, 40), (90, 180, 255)
TIER_COL = [(150, 155, 165), (90, 200, 120), (80, 160, 255), (255, 110, 70)]   # 순정/스트리트/스포츠/레이스
ACCENT = (255, 196, 0)
TEXT = (230, 235, 240)
DIM = (150, 160, 172)
GOOD, BAD = (110, 235, 140), (255, 110, 90)


def _interp(curve, rpm, col):
    """곡선 [(rpm, ...)] 에서 rpm 위치의 col 번째 값 (직선 보간, CARLA 와 같은 방식)."""
    for (r0, *a), (r1, *b) in zip(curve, curve[1:]):
        if rpm <= r1:
            f = 0.0 if r1 == r0 else (rpm - r0) / (r1 - r0)
            return a[col - 1] + (b[col - 1] - a[col - 1]) * f
    return curve[-1][col]


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
        self.fine_i, self._tune_before = 0, {}
        self.d_rpm, self.d_thr, self.d_auto = 0.0, 0.0, False   # 다이노: 회전수, 스로틀, 풀스로틀 측정 중
        self.d_trace = {}                      # 풀스로틀로 측정한 값 {rpm 100 단위: (rpm, Nm, hp)}
        self.dyno_sound = None                 # 다이노 페이지 엔진음용 {rpm, vx, power_kw, throttle}
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
        self._dyno_tick(dt)
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

    def _spec(self, installed=None):
        kind, pt = self._kind_pt()
        return P.apply_parts(self.app.specs[kind], self._installed() if installed is None else installed, pt)

    def _perf(self, installed=None):
        kind, pt = self._kind_pt()
        return P.performance(self._spec(installed), kind, pt)

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
        elif self.page == "fine":
            keys = self._tune_keys()
            shift = bool(getattr(ev, "mod", 0) & pygame.KMOD_SHIFT)
            if up or down:
                self.fine_i = (self.fine_i + (1 if down else -1)) % len(keys)
            elif left or right:
                self._adjust(keys[self.fine_i], (1 if right else -1) * (10 if shift else 1))
            elif k == pygame.K_r:
                self._tune().pop(keys[self.fine_i], None)
            elif back or enter:
                self._leave_fine()
        elif self.page == "dyno":                    # W 스로틀은 _dyno_tick 에서 눌린 상태로 읽음
            if enter or k == pygame.K_SPACE:         # 풀스로틀 측정
                self.d_auto, self.d_trace = True, {}
            elif k == pygame.K_r:
                self.d_trace = {}
            elif back:
                self.dyno_sound, self.page = None, "main"
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
        elif key == "fine":
            self.page, self.fine_i = "fine", 0
            self._tune_before = copy.deepcopy(self._installed())
            pygame.key.set_repeat(300, 80)        # 누르고 있으면 계속 조절
        elif key == "dyno":
            self.page, self.d_thr, self.d_auto, self.d_trace = "dyno", 0.0, False, {}
            self.d_rpm = self._dyno_lo(self._spec(), self._kind_pt()[1])
        elif key == "vehicle":
            self.page, self.veh_row = "vehicle", 0
        elif key == "course":
            self.page, self.course_i = "course", LEVELS.index(self.app.cfg["level"])
        elif key == "settings":
            self.app.menu.open = True
        elif key == "quit":
            return "quit"
        return None

    # ------------------------------------------------------------------ 세부 조정
    def _tune_keys(self):
        kind, pt = self._kind_pt()
        return P.tune_keys(self.app.specs[kind], pt)

    def _tune(self):
        """지금 차종·파워트레인의 정밀 튜닝 dict (없으면 만들어서)."""
        kind, pt = self._kind_pt()
        return self.app.parts.setdefault(kind, {}).setdefault("tune", {}).setdefault(pt, {})

    def _adjust(self, key, steps):
        t = self._tune()
        if key == "redline":
            lo, hi = P.TUNE_REDLINE
            v = max(lo, min(hi, t.get(key, 0) + 50 * steps))
            t[key] = v
            if v == 0:
                t.pop(key)
            return
        lo, hi = P.TUNE_MUL
        v = round(max(lo, min(hi, t.get(key, 1.0) + 0.005 * steps)), 3)
        t[key] = v
        if v == 1.0:
            t.pop(key)

    def _leave_fine(self):
        """세부 조정을 마치면 저장하고 차에 적용."""
        pygame.key.set_repeat(0)
        self.page = "main"
        if self._installed() != self._tune_before:
            P.save_parts(self.app.parts)
            self.app.world.set_vehicle()
            self._say(tr("Fine tuning saved"))

    # ------------------------------------------------------------------ 다이노
    def _dyno_curve(self, spec, pt):
        """[(rpm, Nm, hp)] — CARLA 에 넣는 토크 곡선 그대로 (carla_sim/physics.py)."""
        curve, max_rpm = curve_for(spec, pt)[:2]
        return [(r, t, t * r * 2 * math.pi / 60 / 745.7) for r, t in curve], max_rpm

    @staticmethod
    def _dyno_lo(spec, pt):
        return 0.0 if pt == "ev" else float(spec["idle_rpm"])

    def _dyno_tick(self, dt):
        """다이노 엔진: 스로틀만큼 목표 회전수로, 올라갈 때는 그 회전수의 토크에 비례해 빨리 오른다.
        최고 회전에서는 회전 제한(rev limiter)에 걸려 살짝 튕긴다. 풀스로틀 구간은 측정 곡선에 기록."""
        self.dyno_sound = None
        if self.page != "dyno":
            return
        spec, pt = self._spec(), self._kind_pt()[1]
        curve, max_rpm = self._dyno_curve(spec, pt)
        lo = self._dyno_lo(spec, pt)
        keys = pygame.key.get_pressed()
        want = 1.0 if self.d_auto or keys[pygame.K_w] or keys[pygame.K_UP] else 0.0
        self.d_thr += max(-dt / 0.15, min(dt / 0.15, want - self.d_thr))        # 페달 끝까지 0.15초
        span = max_rpm - lo
        nm = _interp(curve, self.d_rpm, 1)
        target = lo + span * self.d_thr
        if target > self.d_rpm:
            self.d_rpm = min(target, self.d_rpm + span / DYNO_SWEEP * nm / max(c[1] for c in curve) * dt)
        else:
            self.d_rpm = max(target, self.d_rpm - span / DYNO_FALL * dt)
        nm = _interp(curve, self.d_rpm, 1)
        if self.d_thr > 0.98:                        # 풀스로틀 구간 기록 (최고 회전까지)
            b = int(self.d_rpm // 100)
            self.d_trace[b] = (self.d_rpm, nm, nm * self.d_rpm * 2 * math.pi / 60 / 745.7)
        if self.d_rpm >= max_rpm - 1:
            self.d_rpm = max_rpm - span * 0.03                                   # 회전 제한: 연료 차단
            self.d_auto = False                                                    # 풀스로틀 측정 끝
            nm = _interp(curve, self.d_rpm, 1)
        ratio = spec["ev"]["reduction"] if pt == "ev" else spec["gears"][-1] * spec["final_drive"]
        v = self.d_rpm / ratio * 2 * math.pi / 60 * spec["wheel_radius"] if pt == "ev" else 0.0   # 모터음 높이용
        self.dyno_sound = dict(rpm=max(self.d_rpm, 1.0), vx=v, throttle=self.d_thr,
                               power_kw=self.d_thr * nm * self.d_rpm * 2 * math.pi / 60 / 1000)

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
                 "course": "Up/Down course   Enter go   Esc back",
                 "fine": "Up/Down item   Left/Right adjust (Shift = x10)   R reset   Esc save and back",
                 "dyno": "W throttle   Enter/Space full-throttle pull   R clear   Esc back"}
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

    def _page_fine(self, surf, W, H, LW):
        self._text(surf, tr("Fine tuning"), (24, 96), ACCENT, self.f_big)
        pt = self._kind_pt()[1]
        spec, t = self._spec(), self._tune()
        y = 136
        for i, key in enumerate(self._tune_keys()):
            on = i == self.fine_i
            self._row(surf, y, 30, on, LW)
            label = (tr("Gear {n}", n=key[4:]) if key.startswith("gear") else
                     tr("Reduction ratio") if key == "final" and pt == "ev" else tr(TUNE_LABEL[key]))
            self._text(surf, label, (24, y + 5), TEXT if on else (205, 210, 216), self.f_sm)
            val = P.tune_value(spec, pt, key)
            self._text(surf, f"<  {val}  >" if on else val, (LW - 92, y + 4), ACCENT if on else TEXT, self.f_sm,
                       right=True)
            if key in t:
                d = t[key]
                txt = f"{d:+.0f}" if key == "redline" else f"{(d - 1) * 100:+.1f}%"
                ratio = key == "final" or key.startswith("gear")      # 기어비는 좋고 나쁨이 없음
                col = TORQUE_COL if ratio else GOOD if (d > 0 if key == "redline" else d > 1) else BAD
                self._text(surf, txt, (LW - 14, y + 5), col, self.f_sm, right=True)
            y += 32
        self._text(surf, tr("Left/Right 0.5%   Shift 5%   R reset"), (24, y + 8), DIM, self.f_sm)

    def _page_dyno(self, surf, W, H, LW):
        self._text(surf, tr("Dyno test"), (24, 96), ACCENT, self.f_big)
        kind, pt = self._kind_pt()
        spec = self._spec()
        curve, max_rpm = self._dyno_curve(spec, pt)
        stock, stock_max = self._dyno_curve(P.apply_parts(self.app.specs[kind], {}, pt), pt)
        top = max(max_rpm, stock_max)
        meas = [self.d_trace[k_] for k_ in sorted(self.d_trace)]
        src = meas if len(meas) > 1 else curve     # 측정값이 있으면 측정값 기준
        pk_hp = max(src, key=lambda c: c[2])
        pk_nm = max(src, key=lambda c: c[1])
        rpm, thr = self.d_rpm, self.d_thr
        now_nm = _interp(curve, rpm, 1) * thr
        rows = [("Now", f"{rpm:.0f} rpm   {now_nm * rpm * 2 * math.pi / 60 / 745.7:.0f} hp   {now_nm:.0f} Nm"),
                ("Peak power" if len(meas) > 1 else "Peak power (calc.)", f"{pk_hp[2]:.0f} hp @ {pk_hp[0]:.0f} rpm"),
                ("Peak torque" if len(meas) > 1 else "Peak torque (calc.)", f"{pk_nm[1]:.0f} Nm @ {pk_nm[0]:.0f} rpm"),
                ("Wheel power", f"{pk_hp[2] * spec['drivetrain_eff']:.0f} hp"),
                ("Max rpm", f"{max_rpm:.0f} rpm")]
        y = 136
        for label, val in rows:
            self._text(surf, tr(label), (24, y), DIM, self.f_sm)
            self._text(surf, val, (24, y + 18), TEXT, self.f)
            y += 48
        # 스로틀 막대
        self._text(surf, tr("Throttle {p:.0f}%", p=thr * 100), (24, y + 2), DIM, self.f_sm)
        pygame.draw.rect(surf, (45, 50, 58), (24, y + 24, LW - 48, 12))
        pygame.draw.rect(surf, (255, 80, 60) if thr > 0.98 else POWER_COL, (24, y + 24, int((LW - 48) * thr), 12))
        y += 50
        for s_, col in (("W (hold): throttle", TEXT), ("Enter / Space: full-throttle pull", ACCENT), ("R: clear graph", DIM)):
            self._text(surf, tr(s_), (24, y), col, self.f_sm)
            y += 22
        # 그래프 (가운데 아래): 주황 = 출력(hp), 파랑 = 토크(Nm), 흐린 선 = 순정
        gx, gw = LW + 8, W - LW - 390 - 16
        gy, gh = H - 34 - 250, 236
        self._shade(surf, (gx, gy, gw, gh), 225, (10, 12, 16))
        px, py, pw, ph = gx + 44, gy + 30, gw - 92, gh - 58
        hp_top = max(max(c[2] for c in curve), max(c[2] for c in stock)) * 1.1
        nm_top = max(max(c[1] for c in curve), max(c[1] for c in stock)) * 1.1
        for f in (0.25, 0.5, 0.75, 1.0):
            pygame.draw.line(surf, (40, 44, 52), (px, py + ph * (1 - f)), (px + pw, py + ph * (1 - f)))
        self._text(surf, "hp", (gx + 8, gy + 8), POWER_COL, self.f_sm)
        self._text(surf, "Nm", (gx + gw - 8, gy + 8), TORQUE_COL, self.f_sm, right=True)
        self._text(surf, f"{hp_top:.0f}", (px - 4, py - 8), POWER_COL, self.f_sm, right=True)
        self._text(surf, f"{nm_top:.0f}", (px + pw + 4, py - 8), TORQUE_COL, self.f_sm)
        for f in (0.0, 0.5, 1.0):
            self._text(surf, f"{top * f:.0f}", (px + pw * f - 16, py + ph + 4), DIM, self.f_sm)
        self._text(surf, "rpm", (px + pw // 2 + 40, py + ph + 4), DIM, self.f_sm)

        def line(c, col, top_v, color, upto=None, width=1):
            p_ = [(px + pw * x[0] / top, py + ph * (1 - x[col] / top_v)) for x in c if upto is None or x[0] <= upto]
            if upto is not None and upto < c[-1][0]:
                p_.append((px + pw * upto / top, py + ph * (1 - _interp(c, upto, col) / top_v)))
            if len(p_) > 1:
                pygame.draw.lines(surf, color, False, p_, width)

        line(stock, 2, hp_top, (110, 80, 50))
        line(stock, 1, nm_top, (50, 80, 110))
        line(curve, 2, hp_top, POWER_COL)            # 계산된 풀스로틀 곡선 (얇게)
        line(curve, 1, nm_top, TORQUE_COL)
        if len(meas) > 1:                            # 풀스로틀로 측정한 구간 (굵게)
            line(meas, 2, hp_top, POWER_COL, width=4)
            line(meas, 1, nm_top, TORQUE_COL, width=4)
        x = px + pw * rpm / top
        pygame.draw.line(surf, (235, 235, 235), (x, py), (x, py + ph), 1)
        self._text(surf, tr("faint = stock"), (gx + gw // 2, gy + 8), DIM, self.f_sm)

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
        elif self.page == "fine":                    # 세부 조정 전 값과 비교
            cur = self._perf(self._tune_before)
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
