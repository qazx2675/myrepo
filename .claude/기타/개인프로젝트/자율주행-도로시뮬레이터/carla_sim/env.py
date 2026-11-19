"""CARLA 버전 외부 컨트롤러 인터페이스 (향후 CUDA 자율주행 코드가 붙는 곳). 기존 env.py 와 같은 모양.

1) 내비게이션 방식: obs = CarlaSim.observe() (차선 중심/양쪽 선, 경로, 안내, 주변 차량, 선행차 ...)
2) 화면 캡처 방식: frame = (H, W, 3) uint8 RGB, CARLA 카메라 센서 영상 (HUD 없음)

    from carla_sim.env import CarlaDrivingEnv
    env = CarlaDrivingEnv(level="normal", vehicle="car", powertrain="ev", frame_size=(640, 360))
    obs, frame = env.reset()
    env.sim.set_destination(0)
    while True:
        obs, frame = env.step((throttle, brake, steer))     # steer +는 왼쪽
    env.close()

CARLA 서버(CarlaUE4.exe)가 먼저 실행 중이어야 한다.
주의: 한 파이썬 프로세스 안에서 코스(맵)를 두 번 이상 바꾸면 CARLA 0.9.16 클라이언트가 가끔
"_episode != nullptr" 로 비정상 종료한다. 같은 코스로 reset() 하는 것은 괜찮고, 코스를 바꿀 때는
프로세스를 새로 띄우십시오 (run.py 도 코스 전환 시 프로세스를 다시 시작한다).
"""
import carla

from carla_sim.sim import CarlaSim
from sim.vehicle import load_specs


class CarlaDrivingEnv:
    def __init__(self, level="normal", vehicle="car", powertrain="auto", n_cars=None, n_lanes=3, seed=0,
                 frame_size=(640, 360), render_frames=True, view="cockpit", host="127.0.0.1", port=2000):
        self.client = carla.Client(host, port)
        self.client.set_timeout(60.0)
        self.args = dict(level=level, vehicle=vehicle, powertrain=powertrain, cars=n_cars, seed=seed,
                         lanes=n_lanes, view=view, frame_size=frame_size, camera=render_frames)
        self.sim = None

    def reset(self):
        if self.sim is not None:
            self.sim.destroy()
        self.sim = CarlaSim(self.client, specs=load_specs(), **self.args)
        self.sim.step()
        return self.observe(), self.render_frame()

    def step(self, action, substeps=1):
        throttle, brake, steer = action
        for _ in range(substeps):
            self.sim.step(throttle, brake, steer)
        return self.observe(), self.render_frame()

    def observe(self):
        return self.sim.observe()

    def render_frame(self):
        return self.sim.frame

    def close(self):
        if self.sim is not None:
            self.sim.destroy()
            self.sim = None
