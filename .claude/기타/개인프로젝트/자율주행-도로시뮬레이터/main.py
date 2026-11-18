"""자율주행 베이스 도로 시뮬레이터 실행 파일.

  python main.py                                   # 창 띄워서 직접 운전 (중급)
  python main.py --level hard --vehicle bus --powertrain hybrid
  python main.py --level long --cars 0             # 8 km 직선 코스, 제로백 측정
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


def run_window(args, controller=None, size=(1280, 720)):
    if args.screenshot:
        os.environ.setdefault("SDL_VIDEODRIVER", "dummy")
        os.environ.setdefault("SDL_AUDIODRIVER", "dummy")
    import pygame
    from viz.hud import Hud
    from viz.render3d import CameraFX, draw_scene
    from viz.sound import Sound
    from viz.topdown import draw_topdown

    pygame.init()
    screen = pygame.display.set_mode(size)
    pygame.display.set_caption("Autodrive road sim (base)")
    clock = pygame.time.Clock()
    level, view = args.level, args.view
    world = make_world(level, args.vehicle, args.powertrain, args.cars, args.seed, args.lanes)
    hud = Hud(world)
    fx = CameraFX()
    sound = Sound() if not args.screenshot else None
    gas, brk_pedal = Pedal(0.7, 1.8), Pedal(0.35, 1.4)
    paused = False
    env = None
    if controller is not None:
        from env import DrivingEnv
        env = DrivingEnv()
        env.world = world
    ext_on = controller is not None

    def render(fps):
        if view == "top":
            draw_topdown(screen, world)
        else:
            draw_scene(screen, world, view, fx)
        hud.draw(screen, world, fps, paused, view)

    if args.screenshot:  # 자동주행(또는 외부 컨트롤러)으로 seconds 만큼 진행 후 한 장 저장
        if hasattr(world, "track"):
            world.ego_mode = "auto"
            if args.dest is not None:
                world.nav.set_destination(args.dest)
        for _ in range(int(args.seconds / DT)):
            if controller is not None:
                frame = env.render_frame() if getattr(controller, "needs_frame", False) else None
                world.ego_mode = "manual"
                world.mode_label = "EXTERNAL"
                world.step(DT, *controller(env.observe(), frame))
            elif hasattr(world, "track"):
                world.step(DT)
            else:
                world.step(DT, 0.3, 0.0, 0.0)
            fx.update(world.ego, DT)
        hud.show_help = False
        render(60.0)
        pygame.image.save(screen, args.screenshot)
        print("saved", args.screenshot)
        return 0

    while True:
        for ev in pygame.event.get():
            if ev.type == pygame.QUIT:
                return 0
            if ev.type != pygame.KEYDOWN:
                continue
            k = ev.key
            road = hasattr(world, "track")
            if k == pygame.K_ESCAPE:
                return 0
            elif k == pygame.K_v:
                view = VIEWS[(VIEWS.index(view) + 1) % len(VIEWS)]
            elif k == pygame.K_g:
                world.ego.toggle_reverse()
            elif k == pygame.K_z:
                world.ego.shift(-1)
            elif k == pygame.K_x:
                world.ego.shift(+1)
            elif k == pygame.K_t:
                world.cycle_vehicle()
            elif k == pygame.K_p:
                world.cycle_powertrain()
            elif k == pygame.K_BACKSPACE:
                world.reset_ego()
            elif k == pygame.K_h:
                hud.show_help = not hud.show_help
            elif k == pygame.K_c:
                fx.enabled = not fx.enabled
            elif k == pygame.K_o and sound:
                sound.enabled = not sound.enabled
            elif k == pygame.K_SPACE:
                paused = not paused
            elif k == pygame.K_k and controller is not None:
                ext_on = not ext_on
            elif pygame.K_F1 <= k <= pygame.K_F5:
                level = LEVEL_KEYS[k - pygame.K_F1]
                e = world.ego
                world = make_world(level, e.kind, e.powertrain, None, args.seed, args.lanes)
                hud.set_world(world)
                if env is not None:
                    env.world = world
            elif k == pygame.K_n:
                if road:
                    world.next_destination()
                else:
                    world.next_target()
            elif road and k == pygame.K_m:
                world.cycle_mode()
            elif road and k == pygame.K_q:
                world.change_lane(-1)
            elif road and k == pygame.K_e:
                world.change_lane(+1)
            elif road and k in (pygame.K_LEFTBRACKET, pygame.K_RIGHTBRACKET):
                cur = world.ego_set_speed or world.track.limit_at(world.ego.s) / 3.6
                d = 10 / 3.6 if k == pygame.K_RIGHTBRACKET else -10 / 3.6
                world.ego_set_speed = min(250 / 3.6, max(10 / 3.6, cur + d))

        keys = pygame.key.get_pressed()
        throttle = gas.update(keys[pygame.K_w] or keys[pygame.K_UP], DT)
        brake = brk_pedal.update(keys[pygame.K_s] or keys[pygame.K_DOWN], DT)
        steer = (1.0 if (keys[pygame.K_a] or keys[pygame.K_LEFT]) else 0.0) - \
                (1.0 if (keys[pygame.K_d] or keys[pygame.K_RIGHT]) else 0.0)
        world.mode_label = None
        if ext_on and not paused:
            frame = env.render_frame() if getattr(controller, "needs_frame", False) else None
            throttle, brake, steer = controller(env.observe(), frame)
            world.ego_mode = "manual"
            world.mode_label = "EXTERNAL"
        if not paused:
            world.step(DT, throttle, brake, steer)
            fx.update(world.ego, DT)
        if sound:
            sound.update(world.ego, getattr(world, "pedals", (throttle, 0.0))[0])
        render(clock.get_fps())
        pygame.display.flip()
        clock.tick(60)


def main():
    ap = argparse.ArgumentParser(description="자율주행 베이스 도로 시뮬레이터")
    ap.add_argument("--level", choices=LEVEL_KEYS, default="normal",
                    help="easy/normal/hard = 초급/중급/고급, long = 8 km 직선 코스, parking = 주차장")
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
