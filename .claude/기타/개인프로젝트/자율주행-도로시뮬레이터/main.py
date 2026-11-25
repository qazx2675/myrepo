"""자율주행 베이스 도로 시뮬레이터 실행 파일.

  python main.py                                   # 창 띄워서 직접 운전 (중급)
  python main.py --level hard --vehicle bus --powertrain hybrid
  python main.py --level long --cars 0             # 30 km 직선 코스, 제로백 측정
  python main.py --level parking                   # 주차장
  python main.py --controller examples/nav_follow.py    # 외부 컨트롤러가 운전 (K 로 on/off)
  python main.py --headless --seconds 600          # 화면 없이 자동주행 + 통계
  python main.py --screenshot shot.png --view chase --seconds 20
"""
import argparse
import importlib.util
import os
import sys
import time

from env import LEVELS as LEVEL_KEYS
from env import make_world
from sim.vehicle import load_specs

DT = 1.0 / 60.0
VIEWS = ("cockpit", "chase", "top")


def load_controller(path):
    """파이썬 파일에서 Controller 클래스를 읽어 인스턴스를 만든다."""
    spec = importlib.util.spec_from_file_location("ext_controller", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod.Controller()


class Pedal:
    """키보드(0/1) 입력을 실제 페달처럼: 서서히 밟히고(지수 곡선), 떼면 빨리 복귀."""

    def __init__(self, press_time, expo):
        self.pos, self.press_time, self.expo = 0.0, press_time, expo

    def update(self, pressed, dt):
        if pressed:
            self.pos = min(1.0, self.pos + dt / self.press_time)
        else:
            self.pos = max(0.0, self.pos - dt / 0.15)
        return self.pos ** self.expo


def run_headless(world, seconds):
    world.ego_mode = "auto"
    t0 = time.time()
    max_overlap, min_gap, v_sum, steps = 0, float("inf"), 0.0, int(seconds / DT)
    for k in range(steps):
        world.step(DT)
        v_sum += world.ego.vx
        min_gap = min(min_gap, world.gap[0])
        if k % 60 == 0:
            max_overlap = max(max_overlap, world.ai_overlaps())
    print(f"simulated {seconds:.0f}s ({steps} steps) in {time.time() - t0:.1f}s wall")
    print(f"level / track      {world.level['name']} / {world.track.L:.0f} m, lanes {world.track.n_lanes}")
    print(f"ego                {world.ego.spec['name']} {world.ego.powertrain}")
    print(f"ego avg speed      {v_sum / steps * 3.6:8.1f} km/h, laps {world.odometer / world.track.L:.2f}")
    print(f"ego min gap        {min_gap:8.1f} m")
    print(f"AI avg speed       {world.v[1:].mean() * 3.6:8.1f} km/h")
    print(f"AI lane changes    {world.lane_changes:8d}")
    print(f"ego collisions     {world.collisions:8d}")
    print(f"AI-AI overlaps max {max_overlap:8d}")
    return 0 if (world.collisions == 0 and max_overlap == 0) else 1


class App:
    """창 모드 상태: 월드, 설정값, 렌더러, HUD, 메뉴."""

    def __init__(self, args, controller, size):
        from viz.hud import Hud
        from viz.menu import SettingsMenu
        from viz.render3d import CameraFX
        from viz.scene import make_scene_renderer
        from viz.sound import Sound

        self.cfg = dict(level=args.level, cars=args.cars, lanes=args.lanes, seed=args.seed)
        self.specs = load_specs()
        self.view = args.view
        self.world = make_world(args.level, args.vehicle, args.powertrain, args.cars, args.seed, args.lanes,
                                specs=self.specs)
        self.hud = Hud(self.world)
        self.fx = CameraFX()
        self.sound = Sound() if not args.screenshot else None
        self.scene = make_scene_renderer(args.renderer, size)
        self.menu = SettingsMenu(self)
        self.controller = controller
        self.env = None
        if controller is not None:
            from env import DrivingEnv
            self.env = DrivingEnv()
            self.env.world = self.world

    def rebuild(self):
        """코스/난이도/트래픽 설정이 바뀌면 월드를 새로 만든다 (차종·파워트레인·제원 유지)."""
        e = self.world.ego
        c = self.cfg
        self.world = make_world(c["level"], e.kind, e.powertrain, c["cars"], c["seed"], c["lanes"], specs=self.specs)
        self.hud.set_world(self.world)
        if self.env is not None:
            self.env.world = self.world

    def render(self, screen, fps, paused):
        from viz.topdown import draw_topdown
        if self.view == "top":
            draw_topdown(screen, self.world)
        else:
            self.scene.draw(screen, self.world, self.view, self.fx)
        self.hud.draw(screen, self.world, fps, paused, self.view)
        if self.menu.open:
            self.menu.draw(screen)

    def ext_action(self):
        c = self.controller
        frame = self.env.render_frame() if getattr(c, "needs_frame", False) else None
        self.world.ego_mode = "manual"
        self.world.mode_label = "EXTERNAL"
        return c(self.env.observe(), frame)


def run_window(args, controller=None, size=(1280, 720)):
    if args.screenshot:
        os.environ.setdefault("SDL_VIDEODRIVER", "dummy")
        os.environ.setdefault("SDL_AUDIODRIVER", "dummy")
    import pygame
    pygame.init()
    screen = pygame.display.set_mode(size)
    pygame.display.set_caption("Autodrive road sim")
    clock = pygame.time.Clock()
    app = App(args, controller, size)
    gas, brk_pedal = Pedal(0.7, 1.8), Pedal(0.35, 1.4)
    paused = False
    ext_on = controller is not None

    if args.screenshot:  # 자동주행(또는 외부 컨트롤러)으로 seconds 만큼 진행 후 한 장 저장
        w = app.world
        if hasattr(w, "track"):
            w.ego_mode = "auto"
            if args.dest is not None:
                w.nav.set_destination(args.dest)
        for _ in range(int(args.seconds / DT)):
            if controller is not None:
                w.step(DT, *app.ext_action())
            elif hasattr(w, "track"):
                w.step(DT)
            else:
                w.step(DT, 0.3, 0.0, 0.0)
            app.fx.update(w.ego, DT)
        app.hud.show_help = False
        app.menu.open = args.menu
        app.render(screen, 60.0, False)
        pygame.image.save(screen, args.screenshot)
        print("saved", args.screenshot)
        return 0

    while True:
        for ev in pygame.event.get():
            if ev.type == pygame.QUIT:
                return 0
            if ev.type != pygame.KEYDOWN:
                continue
            if app.menu.open:
                if app.menu.handle(ev) == "quit":
                    return 0
                continue
            k = ev.key
            w = app.world
            road = hasattr(w, "track")
            if k in (pygame.K_TAB, pygame.K_ESCAPE):
                app.menu.open = True
            elif k == pygame.K_v:
                app.view = VIEWS[(VIEWS.index(app.view) + 1) % len(VIEWS)]
            elif k == pygame.K_g:
                w.ego.toggle_reverse()
            elif k == pygame.K_z:
                w.ego.shift(-1)
            elif k == pygame.K_x:
                w.ego.shift(+1)
            elif k == pygame.K_t:
                w.cycle_vehicle()
            elif k == pygame.K_p:
                w.cycle_powertrain()
            elif k == pygame.K_BACKSPACE:
                w.reset_ego()
            elif k == pygame.K_h:
                app.hud.show_help = not app.hud.show_help
            elif k == pygame.K_c:
                app.fx.enabled = not app.fx.enabled
            elif k == pygame.K_o and app.sound:
                app.sound.enabled = not app.sound.enabled
            elif k == pygame.K_SPACE:
                paused = not paused
            elif k == pygame.K_k and controller is not None:
                ext_on = not ext_on
            elif pygame.K_F1 <= k <= pygame.K_F5:
                app.cfg["level"] = LEVEL_KEYS[k - pygame.K_F1]
                app.rebuild()
            elif k == pygame.K_n:
                if road:
                    w.next_destination()
                else:
                    w.next_target()
            elif road and k == pygame.K_m:
                w.cycle_mode()
            elif road and k == pygame.K_q:
                w.change_lane(-1)
            elif road and k == pygame.K_e:
                w.change_lane(+1)
            elif road and k in (pygame.K_LEFTBRACKET, pygame.K_RIGHTBRACKET):
                cur = w.ego_set_speed or w.track.limit_at(w.ego.s) / 3.6
                d = 10 / 3.6 if k == pygame.K_RIGHTBRACKET else -10 / 3.6
                w.ego_set_speed = min(250 / 3.6, max(10 / 3.6, cur + d))

        w = app.world
        keys = pygame.key.get_pressed()
        throttle = gas.update(keys[pygame.K_w] or keys[pygame.K_UP], DT)
        brake = brk_pedal.update(keys[pygame.K_s] or keys[pygame.K_DOWN], DT)
        steer = (1.0 if (keys[pygame.K_a] or keys[pygame.K_LEFT]) else 0.0) - \
                (1.0 if (keys[pygame.K_d] or keys[pygame.K_RIGHT]) else 0.0)
        running = not paused and not app.menu.open
        w.mode_label = None
        if ext_on and running:
            throttle, brake, steer = app.ext_action()
        if running:
            w.step(DT, throttle, brake, steer)
            app.fx.update(w.ego, DT)
        if app.sound:
            app.sound.update(w.ego, getattr(w, "pedals", (throttle, 0.0))[0] if running else 0.0)
        app.render(screen, clock.get_fps(), paused)
        pygame.display.flip()
        clock.tick(60)


def main():
    ap = argparse.ArgumentParser(description="자율주행 베이스 도로 시뮬레이터")
    ap.add_argument("--level", choices=LEVEL_KEYS, default="normal",
                    help="easy/normal/hard = 초급/중급/고급, long = 30 km 직선 코스, parking = 주차장")
    ap.add_argument("--vehicle", choices=("car", "bus", "truck"), default="car")
    ap.add_argument("--powertrain", choices=("auto", "manual", "ev", "hybrid"), default="auto")
    ap.add_argument("--cars", type=int, default=None, help="AI 트래픽 차량 수 (기본: 난이도별)")
    ap.add_argument("--lanes", type=int, default=3, help="차로 수")
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--view", choices=VIEWS, default="cockpit")
    ap.add_argument("--dest", type=int, default=None, help="스크린샷/헤드리스용 목적지 번호")
    ap.add_argument("--headless", action="store_true", help="화면 없이 자동주행 시뮬레이션 + 통계")
    ap.add_argument("--seconds", type=float, default=300.0, help="headless/screenshot 시뮬레이션 시간")
    ap.add_argument("--screenshot", help="자동주행 후 한 프레임을 PNG로 저장")
    ap.add_argument("--controller", help="외부 컨트롤러 파이썬 파일 (Controller 클래스 포함)")
    ap.add_argument("--renderer", choices=("gl", "classic"), default="gl",
                    help="gl = OpenGL 3D (조명/그림자), classic = 예전 2D 폴리곤 (OpenGL 안 될 때)")
    ap.add_argument("--menu", action="store_true", help="스크린샷에 설정 메뉴를 띄운 상태로 저장")
    a = ap.parse_args()

    if a.headless:
        if a.level == "parking":
            ap.error("--headless 는 도로 코스에서만 지원")
        world = make_world(a.level, a.vehicle, a.powertrain, a.cars, a.seed, a.lanes)
        if a.dest is not None:
            world.nav.set_destination(a.dest)
        return run_headless(world, a.seconds)
    ctl = load_controller(a.controller) if a.controller else None
    return run_window(a, controller=ctl)


if __name__ == "__main__":
    sys.exit(main())
