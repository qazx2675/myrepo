"""CARLA 버전 실행 파일 (사실적인 그래픽). CARLA 서버(CarlaUE4.exe)를 먼저 켜 둔다.

  python carla_sim/run.py                                    # Town04 고속도로, 중급
  python carla_sim/run.py --level long --cars 0              # 30 km 직선, 제로백 0-100/200/300
  python carla_sim/run.py --level parking                    # 평행 주차
  python carla_sim/run.py --level hard --vehicle truck --powertrain manual
  python carla_sim/run.py --controller examples/nav_follow.py   # 외부 컨트롤러 (K 로 on/off)
"""
import argparse
import math
import os
import sys
import types

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, ROOT)

import carla  # noqa: E402
import pygame  # noqa: E402

from carla_sim.sim import LEVELS, CarlaSim  # noqa: E402
from sim import i18n  # noqa: E402
from sim.i18n import tr  # noqa: E402
from sim.parts import load_parts  # noqa: E402
from viz.menu import LEVEL_NAME, PT_NAME  # noqa: E402
from main import Pedal, load_controller  # noqa: E402
from sim.vehicle import load_specs  # noqa: E402

LEVEL_KEYS = tuple(LEVELS)
VIEWS = ("cockpit", "chase", "top")
FULL = pygame.FULLSCREEN | pygame.SCALED          # 전체화면: 게임 해상도 그대로 모니터에 맞춰 늘림 (GPU 확대)
ARROW = {"left": "<-", "right": "->", "change_left": "<<", "change_right": ">>", "straight": "^", "arrive": "*"}


class CarlaApp:
    """viz/menu.py 설정 메뉴가 기대하는 속성(cfg, specs, world, view, fx, sound, hud, rebuild)을 갖춘 앱."""

    def __init__(self, args, size, screen):
        from carla_sim.garage import Garage
        from viz.menu import SettingsMenu
        from viz.sound import Sound
        self.args, self.size, self.screen = args, size, screen
        self.f_big = i18n.font(42, bold=True)        # 한글 글꼴
        self.f = i18n.font(17)
        self.loading(tr("Connecting to CARLA server ..."))
        self.client = carla.Client(args.host, args.port)
        self.client.set_timeout(120.0)
        self.background(tr("Connecting to CARLA server ..."), self.client.get_server_version)  # 서버 없으면 RuntimeError
        self.cfg = dict(level=args.level, cars=args.cars, lanes=args.lanes, seed=args.seed)
        self.specs = load_specs()
        self.parts = load_parts()                 # 정비소 파츠 (config/garage.json)
        self._view = args.view
        screen_state = dict(garage=True, menu=False)   # 시작 화면 = 정비소
        if args.state:                            # rebuild() 가 넘긴 상태 (코스 전환)
            import json
            with open(args.state, encoding="utf-8") as f:
                st = json.load(f)
            os.remove(args.state)
            self.cfg, self.specs, self._view = st["cfg"], st["specs"], st["view"]
            i18n.set_lang(st.get("lang", "ko"))
            screen_state = st.get("screen", screen_state)
            args.vehicle, args.powertrain = st["kind"], st["powertrain"]
        self.fx = types.SimpleNamespace(enabled=True)
        self.sound = Sound()
        self.hud = types.SimpleNamespace(show_help=True)
        self.world = None
        self._build(args.vehicle, args.powertrain)
        self.menu = SettingsMenu(self)
        self.garage = Garage(self)
        if screen_state["garage"]:
            self.garage.show()
        self.menu.open = screen_state["menu"]

    def loading(self, msg):
        self.screen.fill((12, 16, 22))
        self.text(self.screen, tr("AUTODRIVE ROAD SIM - CARLA"), (60, 60), (255, 210, 60), self.f_big)
        self.text(self.screen, msg, (60, 140))
        self.text(self.screen, tr("(first map load can take 10-60 s)"), (60, 170), (150, 160, 170))
        pygame.display.flip()
        pygame.event.pump()

    def _build(self, kind, powertrain):
        if self.world is not None:
            self.world.destroy()
        c = self.cfg
        self.world = self.background(
            tr("Loading course: {name} ...", name=tr(LEVEL_NAME[c["level"]])),
            lambda: CarlaSim(self.client, c["level"], kind, powertrain, c["cars"], c["seed"], c["lanes"],
                             self.specs, view=self._view, frame_size=self.size, parts=self.parts))

    def background(self, msg, fn):
        """오래 걸리는 CARLA 호출(맵 로딩 등)을 별도 스레드에서 돌리고, 그동안 창 이벤트를 계속 처리해
        Windows 의 '응답 없음' 을 막는다."""
        import threading
        import time
        box = {}

        def run():
            try:
                box["v"] = fn()
            except BaseException as ex:  # noqa: BLE001 - 메인 스레드에서 다시 던짐
                box["e"] = ex
        t = threading.Thread(target=run, daemon=True)
        t.start()
        t0 = time.time()
        while t.is_alive():
            for ev in pygame.event.get():
                if ev.type == pygame.QUIT:
                    os._exit(0)
            dots = "." * (1 + int(time.time() - t0) % 3)
            self.loading(f"{msg} {time.time() - t0:4.0f} s {dots}")
            t.join(0.1)
        if "e" in box:
            raise box["e"]
        return box.get("v")

    def rebuild(self):
        """코스/트래픽 변경: 새 프로세스로 다시 시작한다 (한 프로세스에서 맵을 여러 번 바꾸면 CARLA
        클라이언트가 가끔 비정상 종료하므로). 설정 메뉴에서 바꾼 제원은 임시 파일로 넘긴다."""
        import json
        import subprocess
        import tempfile
        e = self.world.ego
        state = dict(cfg=self.cfg, kind=e.kind, powertrain=e.powertrain, view=self._view, specs=self.specs,
                     lang=i18n.LANG, screen=dict(garage=self.garage.open, menu=self.menu.open))
        fd, path = tempfile.mkstemp(suffix=".json", prefix="autodrive_")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(state, f)
        self.loading(tr("Switching to: {name} ...", name=tr(LEVEL_NAME[self.cfg["level"]])))
        a = self.args
        cmd = [sys.executable, os.path.abspath(__file__), "--state", path, "--host", a.host, "--port", str(a.port),
               "--width", str(a.width), "--height", str(a.height)]
        if a.controller:
            cmd += ["--controller", a.controller]
        if a.fullscreen:
            cmd.append("--fullscreen")
        self.world.destroy()
        pygame.quit()
        subprocess.Popen(cmd)
        os._exit(0)

    @property
    def view(self):
        return self._view

    @view.setter
    def view(self, v):
        self._view = v
        self.world.set_view(v)

    # ------------------------------------------------------------------ HUD
    def text(self, surf, s, pos, col=(235, 240, 245), font=None):
        surf.blit((font or self.f).render(s, True, col), pos)

    def box(self, surf, rect):
        sh = pygame.Surface(rect[2:], pygame.SRCALPHA)
        sh.fill((10, 14, 20, 170))
        surf.blit(sh, rect[:2])

    def draw(self, surf, fps, paused, obs):
        w = self.world
        W, H = surf.get_size()
        if w.frame is not None:
            surf.blit(pygame.surfarray.make_surface(w.frame.swapaxes(0, 1)), (0, 0))
        e = w.ego
        v = w.speed() * 3.6
        ctl = e.actor.get_control()
        gear = "R" if w.reverse else ("N" if ctl.gear == 0 else str(ctl.gear))
        # 속도/기어/RPM/배터리
        self.box(surf, (20, H - 130, 400, 110))
        if e.powertrain == "ev":
            frac, lab = min(1.0, abs(w.power_kw) / e.spec["ev"]["motor_power_kw"]), f"{w.power_kw:+.0f} kW"
        else:
            frac, lab = min(1.0, w.rpm / e.spec["redline_rpm"]), f"{w.rpm:.0f} rpm"
        pygame.draw.rect(surf, (60, 60, 60), (290, H - 120, 14, 90))
        col = (90, 200, 255) if w.power_kw < 0 else ((255, 80, 60) if frac > 0.9 else (240, 200, 60))
        pygame.draw.rect(surf, col, (290, H - 30 - int(90 * frac), 14, int(90 * frac)))
        self.text(surf, lab, (312, H - 120))
        if e.powertrain in ("ev", "hybrid"):
            self.text(surf, tr("BAT {p:4.0f}%", p=w.soc * 100), (312, H - 96), (120, 255, 150) if w.soc > 0.15 else (255, 90, 70))
        self.text(surf, f"{v:5.0f}", (30, H - 125), font=self.f_big)
        self.text(surf, "km/h", (190, H - 100))
        self.text(surf, f"{'M' if e.powertrain == 'manual' else 'D'}{gear}  {tr(e.spec['name'])} "
                        f"{tr(PT_NAME[e.powertrain])}", (30, H - 60))
        lim = obs["speed_limit"] * 3.6
        self.text(surf, tr("limit {v:.0f}", v=lim) + (tr("  set {v:.0f}", v=w.set_speed * 3.6) if w.set_speed else ""),
                  (30, H - 38))
        # 상태
        self.box(surf, (10, 10, 370, 140))
        lines = [tr("MODE  {mode}   [{level}]", mode=tr(w.mode_label or w.mode.upper()),
                    level=tr(LEVEL_NAME[self.cfg["level"]])),
                 tr("MAP   {map}   VIEW {view}", map=w.map.name.split("/")[-1], view=tr(self.view)),
                 tr("LANE  {lane} / {n}   offset {off:+.2f} m", lane=obs["lane"] + 1, n=obs["n_lanes"],
                    off=obs["lateral_offset"]),
                 tr("LEAD  {gap:.0f} m", gap=obs["lead_gap"]) if math.isfinite(obs["lead_gap"]) else tr("LEAD  -"),
                 tr("COLLISION {c}   TRAFFIC {t}", c=w.collisions, t=len(w.traffic)),
                 tr("ODO {km:.2f} km   {fps:.0f} FPS", km=w.odometer / 1000, fps=fps) + (tr("   PAUSED") if paused else "")]
        for i, s in enumerate(lines):
            self.text(surf, s, (20, 18 + 21 * i))
        # 내비 배너
        g = obs["nav"]
        self.box(surf, (W // 2 - 300, 10, 600, 46))
        self.text(surf, f"{ARROW.get(g['arrow'], '')}  {g['instruction']}", (W // 2 - 285, 22),
                  (120, 255, 150) if g["arrived"] else (255, 255, 255))
        if w.time < w.flip_notice:                       # 전복 → 자동으로 다시 세움
            self.box(surf, (W // 2 - 300, 62, 600, 40))
            self.text(surf, tr("Vehicle flipped - set back upright on the road"), (W // 2 - 285, 72), (255, 230, 120))
        # 제로백
        t = w.accel_timer
        if t.results or t.best:
            self.box(surf, (W - 320, 10, 310, 90))
            self.text(surf, tr("0-100 / 200 / 300 km/h"), (W - 310, 16), (255, 210, 60))
            for i, (name, d) in enumerate((("now", t.results), ("best", t.best))):   # 숫자는 칸 맞춰 따로
                self.text(surf, tr(name), (W - 310, 42 + 24 * i))
                for j, m in enumerate(t.MARKS):
                    self.text(surf, f"{d[m]:.2f}" if m in d else "-", (W - 250 + 75 * j, 42 + 24 * i))
        # 주차
        p = obs.get("parking")
        if p:
            self.box(surf, (W - 420, 110, 410, 70))
            self.text(surf, tr("PARKED!") if p["parked"] else tr("{kind} parking (N = next)", kind=tr(p["kind"]).capitalize()),
                      (W - 410, 118),
                      (120, 255, 150) if p["parked"] else (255, 230, 120))
            self.text(surf, tr("along {a:+.1f} m  side {s:+.2f} m  yaw {y:.0f}", a=p["target_along"],
                               s=p["target_lateral"], y=p["yaw_error_deg"]), (W - 410, 146))
        self._minimap(surf, obs, W, H)
        if self.hud.show_help:
            self._help(surf, W)

    def _minimap(self, surf, obs, W, H):
        S, cx, cy, sc = 220, W - 240, H - 240, 0.8          # 내 차 기준, 위쪽이 전방
        self.box(surf, (cx, cy, S, S))
        ox, oy = cx + S // 2, cy + S * 3 // 4

        def pt(x, y):
            return float(ox - y * sc), float(oy - x * sc)   # pygame 은 numpy float32 를 좌표로 안 받음

        for arr, col in ((obs["lane_left"], (200, 200, 200)), (obs["lane_right"], (200, 200, 200)),
                         (obs["route"], (60, 160, 255))):
            pts = [pt(x, y) for x, y in arr if -40 < x < 160 and abs(y) < 130]
            if len(pts) > 1:
                pygame.draw.lines(surf, col, False, pts, 3 if col[2] == 255 else 1)
        for x, y, *_ in obs["vehicles"]:
            if -40 < x < 160 and abs(y) < 130:
                pygame.draw.circle(surf, (255, 120, 80), pt(x, y), 3)
        pygame.draw.polygon(surf, (255, 255, 255), [(ox, oy - 7), (ox - 5, oy + 5), (ox + 5, oy + 5)])

    def _help(self, surf, W):
        keys = ["W/S gas/brake  A/D steer", "G D<->R  Z/X gear (manual)", "M manual -> ACC -> AUTO",
                "N next destination", "[ ] AUTO set speed", "T vehicle  P powertrain", "V camera",
                "F1-F5 level  Tab settings  Esc garage", "K external controller", "Backspace reset  H help", "F11 fullscreen"]
        self.box(surf, (10, 160, 300, 22 * len(keys) + 10))
        for i, s in enumerate(keys):
            self.text(surf, tr(s), (20, 166 + 22 * i), (200, 210, 220))


def single_instance(wait):
    """같은 PC 에서 게임 창이 두 개 뜨지 않게 한다 (같은 CARLA 서버에 클라이언트 둘이 붙으면 서로 충돌).
    로컬 포트 하나를 잡아 두는 방식. 코스 전환으로 다시 뜬 프로세스(wait=True)는 이전 프로세스가 끝날 때까지 기다린다."""
    import socket
    import time
    t0 = time.time()
    while True:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            s.bind(("127.0.0.1", 47913))
            s.listen(1)
            return s
        except OSError:
            s.close()
            if not wait or time.time() - t0 > 15:
                return None
            time.sleep(0.2)


def main():
    ap = argparse.ArgumentParser(description="자율주행 도로 시뮬레이터 - CARLA 버전")
    ap.add_argument("--level", choices=LEVEL_KEYS, default="normal")
    ap.add_argument("--vehicle", choices=("car", "bus", "truck"), default="car")
    ap.add_argument("--powertrain", choices=("auto", "manual", "ev", "hybrid"), default="auto")
    ap.add_argument("--cars", type=int, default=None)
    ap.add_argument("--lanes", type=int, default=3, help="긴 직선 코스 차로 수")
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--view", choices=VIEWS, default="cockpit")
    ap.add_argument("--controller")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=2000)
    ap.add_argument("--width", type=int, default=1280)
    ap.add_argument("--height", type=int, default=720)
    ap.add_argument("--fullscreen", action="store_true", help="전체화면으로 시작 (F11 로 전환)")
    ap.add_argument("--state", help=argparse.SUPPRESS)
    a = ap.parse_args()
    lock = single_instance(wait=bool(a.state))
    if lock is None:
        print("이미 실행 중입니다 (게임 창은 하나만 띄웁니다).")
        return 0

    pygame.init()
    size = (a.width, a.height)
    screen = pygame.display.set_mode(size, FULL if a.fullscreen else 0)
    pygame.display.set_caption("Autodrive road sim - CARLA")
    clock = pygame.time.Clock()
    try:
        app = CarlaApp(a, size, screen)
    except RuntimeError as ex:                     # 서버 미실행 / 시간 초과
        screen.fill((40, 10, 10))
        f = i18n.font(18)
        for i, s in enumerate([tr("Cannot connect to CARLA server at {host}:{port}", host=a.host, port=a.port),
                               str(ex)[:120],
                               tr("Start D:\\CARLA_0.9.16\\CarlaUE4.exe (or run_carla.bat) and try again."),
                               tr("Press any key to close.")]):
            screen.blit(f.render(s, True, (255, 230, 230)), (40, 60 + 30 * i))
        pygame.display.flip()
        while pygame.event.wait().type not in (pygame.KEYDOWN, pygame.QUIT):
            pass
        return 1
    controller = load_controller(a.controller) if a.controller else None
    ext_on = controller is not None
    gas, brk = Pedal(0.7, 1.8), Pedal(0.35, 1.4)
    paused = False
    try:
        while True:
            for ev in pygame.event.get():
                if ev.type == pygame.QUIT:
                    return 0
                if ev.type != pygame.KEYDOWN:
                    continue
                if ev.key == pygame.K_F11:                # 전체화면 ↔ 창 (어느 화면에서나)
                    a.fullscreen = not a.fullscreen
                    pygame.display.set_mode(size, FULL if a.fullscreen else 0)
                    continue
                if app.menu.open:
                    if app.menu.handle(ev) == "quit":
                        return 0
                    continue
                if app.garage.open:
                    if app.garage.handle(ev) == "quit":
                        return 0
                    continue
                k, w = ev.key, app.world
                if k == pygame.K_TAB:
                    app.menu.open = True
                elif k == pygame.K_ESCAPE:
                    app.garage.show()
                elif k == pygame.K_v:
                    app.view = VIEWS[(VIEWS.index(app.view) + 1) % 3]
                elif k == pygame.K_g:
                    w.toggle_reverse()
                elif k in (pygame.K_z, pygame.K_x):
                    w.shift(-1 if k == pygame.K_z else 1)
                elif k == pygame.K_t:
                    w.cycle_vehicle()
                elif k == pygame.K_p:
                    w.cycle_powertrain()
                elif k == pygame.K_m:
                    w.cycle_mode()
                elif k == pygame.K_n:
                    w.next_destination()
                elif k in (pygame.K_LEFTBRACKET, pygame.K_RIGHTBRACKET):
                    w.change_set_speed(10 if k == pygame.K_RIGHTBRACKET else -10)
                elif k == pygame.K_BACKSPACE:
                    w.reset_ego()
                elif k == pygame.K_h:
                    app.hud.show_help = not app.hud.show_help
                elif k == pygame.K_SPACE:
                    paused = not paused
                elif k == pygame.K_k and controller is not None:
                    ext_on = not ext_on
                elif pygame.K_F1 <= k <= pygame.K_F5:
                    app.cfg["level"] = LEVEL_KEYS[k - pygame.K_F1]
                    app.rebuild()
            w = app.world
            keys = pygame.key.get_pressed()
            throttle = gas.update(keys[pygame.K_w] or keys[pygame.K_UP], 1 / 30)
            brake = brk.update(keys[pygame.K_s] or keys[pygame.K_DOWN], 1 / 30)
            steer = float(keys[pygame.K_a] or keys[pygame.K_LEFT]) - float(keys[pygame.K_d] or keys[pygame.K_RIGHT])
            obs = w.observe()
            w.mode_label = None
            if ext_on and not paused and not app.menu.open and not app.garage.open:
                if w.mode != "manual":
                    w.mode = "manual"
                    w._apply_auto()
                w.mode_label = "EXTERNAL"
                throttle, brake, steer = controller(obs, w.frame if getattr(controller, "needs_frame", False) else None)
            running = not paused and not app.menu.open and not app.garage.open
            if app.garage.open and not app.menu.open:
                app.garage.update(1 / 30)
            elif running:
                w.fx_enabled = app.fx.enabled
                w.step(throttle, brake, steer)
            if app.sound:
                app.sound.update(types.SimpleNamespace(
                    vx=w.speed(), vy=w.vy, rpm=w.rpm, powertrain=w.ego.powertrain, engine_on=True,
                    power_kw=w.power_kw, sliding=w.sliding, abs_active=False, wheelspin=False,
                    parts=app.parts.get(w.ego.kind, {}), redline=w.ego.spec["redline_rpm"]),
                    throttle if running else 0.0)
            if app.garage.open:
                app.garage.draw(screen)
            else:
                app.draw(screen, clock.get_fps(), paused, obs)
            if app.menu.open:
                app.menu.draw(screen)
            pygame.display.flip()
            clock.tick(30)
    finally:
        app.world.destroy()


if __name__ == "__main__":
    sys.exit(main())
