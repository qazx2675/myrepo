"""CARLA 월드 래퍼: 맵/트래픽/내 차/카메라/내비/주차/제로백을 한 곳에서 관리.

CARLA 서버(CarlaUE4.exe)가 먼저 떠 있어야 한다. 동기 모드(고정 1/60초)로 돌려서
step() 한 번 = 물리 한 스텝 = 카메라 한 프레임이 정확히 맞는다 (CUDA 학습/평가에 필요).
"""
import math
import queue
import random

import carla
import numpy as np

from carla_sim.physics import apply_specs
from carla_sim.route import STEP, RoutePlanner, guidance
from carla_sim.xodr import long_straight_xodr, parking_lot_xodr
from sim.controllers import idm_accel
from sim.i18n import tr
from sim.parts import PAINT, apply_parts, tier
from sim.vehicle import AccelTimer

DT = 1.0 / 60.0                 # 동기 모드 한 스텝 = 화면 한 프레임 (60 FPS)
ROLE = "autodrive"          # 다른 차/주차된 차 role_name (강제 종료 후 남은 차 정리용, 내 차는 "hero")

# 난이도/코스 → 맵, 트래픽, Traffic Manager 성향, 날씨
LEVELS = {
    "easy":    dict(map="Town04", cars=25, speed_diff=20, gap=4.0, lane_change=0, weather="ClearNoon"),
    "normal":  dict(map="Town04", cars=70, speed_diff=0, gap=2.5, lane_change=5, weather="CloudyNoon"),
    "hard":    dict(map="Town05", cars=110, speed_diff=-20, gap=1.2, lane_change=20, weather="WetCloudySunset"),
    "long":    dict(map=None, cars=20, speed_diff=0, gap=3.0, lane_change=3, weather="ClearNoon"),
    "parking": dict(map="lot", cars=0, speed_diff=0, gap=3.0, lane_change=0, weather="ClearNoon"),
}
LONG_LENGTH = 30000.0

# 차종/파워트레인 → CARLA 블루프린트 (없으면 같은 종류 아무거나)
BLUEPRINTS = {
    ("car", "ev"): "vehicle.tesla.model3",
    "car": "vehicle.lincoln.mkz_2020",
    "bus": "vehicle.mitsubishi.fusorosa",
    "truck": "vehicle.carlamotors.european_hgv",
}


class Ego:
    """설정 메뉴(viz/menu.py)가 보는 내 차 정보."""

    def __init__(self, kind, powertrain, spec, actor):
        self.kind, self.powertrain, self.spec, self.actor = kind, powertrain, spec, actor


def _yaw_vectors(yaw_deg):
    r = math.radians(yaw_deg)
    return (math.cos(r), math.sin(r)), (-math.sin(r), math.cos(r))   # 전방, 오른쪽 (CARLA 좌표)


class CarlaSim:
    def __init__(self, client, level, vehicle, powertrain, cars, seed, lanes, specs,
                 view="cockpit", frame_size=(1280, 720), camera=True, parts=None):
        self.client, self.level_key, self.specs = client, level, specs
        self.parts = parts if parts is not None else {}      # 정비소 장착 파츠 {차종: {파츠: 단계}}
        self.cfg = LEVELS[level]
        self.n_cars = self.cfg["cars"] if cars is None else cars
        self.seed, self.lanes, self.frame_size, self.view = seed, lanes, frame_size, view
        self.rng = random.Random(seed)
        self.lot = self._lot_dims(specs[vehicle])
        self.world = self._load_world()
        self.map = self.world.get_map()
        self.planner = RoutePlanner(self.map)
        self.traffic, self.parked = [], []
        self.camera, self.frame, self._frames = None, None, queue.Queue()
        self.want_camera = camera
        self.mode = "manual"                # manual / acc(속도·차간만 자동) / auto (Traffic Manager 가 운전)
        self.gear, self.reverse = 1, False
        self.set_speed = None               # AUTO 목표 속도 [m/s]
        self.collisions, self.time, self.odometer = 0, 0.0, 0.0
        self.accel_timer = AccelTimer()
        self.route, self.route_idx, self.dest_name, self.dest_i = [], 0, None, -1
        self.parking_target, self.parking_targets, self.parking_i = None, [], 0
        self.mode_label = None
        self.fx_enabled = True
        self._fx = dict(ax=0.0, ay=0.0, look=0.0, t=0.0)
        self._studio, self._hidden = None, []      # 정비소에 들어오기 전 위치, 정비소 동안 숨긴 주변 구조물
        self.v_prev, self.ax, self.vy, self.power_kw, self.rpm = 0.0, 0.0, 0.0, 0.0, 0.0
        self.sliding = False
        self._last_gear = 1
        self._timer_reset = False                         # 리셋 직후 0-100 측정을 정지 상태부터 다시
        self.dt, self._dt_changed = DT, 0.0              # 한 스텝 시간 (서버가 60 FPS 를 못 내면 늘려서 실시간 유지)
        self._flip_t, self.flip_notice = 0.0, -1.0       # 전복 지속 시간 / 다시 세운 안내를 보일 때까지(시뮬 시각)
        self.ego = None
        self._spawn_ego(vehicle, powertrain, self._start_transform())
        self.soc = self._full_soc()
        if level == "parking":
            self._setup_parking()
        self._spawn_traffic()
        self.dests = self._destinations()
        self.world.tick()

    # ------------------------------------------------------------------ 맵/월드
    @staticmethod
    def _lot_dims(spec):
        """주차칸 크기를 내 차에 맞춘다 (버스/트럭도 들어가게)."""
        return dict(perp_w=spec["width"] + 0.9, perp_d=max(6.0, spec["length"] + 1.0),
                    par_w=max(2.6, spec["width"] + 0.7), par_l=spec["length"] + 2.5,
                    aisle=max(3.6, spec["width"] + 1.6))

    def _load_world(self):
        # 맵 전환 전 동기 모드를 끈다 (동기 모드 + Traffic Manager 상태에서 load_world 하면 CARLA 클라이언트가
        # "_episode != nullptr" 로 비정상 종료하는 경우가 있음). TM 은 맵을 불러온 뒤에 가져온다.
        cur = self.client.get_world()
        s = cur.get_settings()
        if s.synchronous_mode:
            s.synchronous_mode, s.fixed_delta_seconds = False, None
            cur.apply_settings(s)
        if self.cfg["map"] in (None, "lot"):
            params = carla.OpendriveGenerationParameters(
                vertex_distance=2.0, max_road_length=500.0, wall_height=1.0, additional_width=0.6,
                smooth_junctions=True, enable_mesh_visibility=True)
            L = self.lot
            xodr = long_straight_xodr(LONG_LENGTH, self.lanes) if self.cfg["map"] is None else \
                parking_lot_xodr(160.0, L["aisle"], L["par_w"], L["perp_d"])
            same = cur.get_map().to_opendrive().strip() == xodr.strip()   # 같은 코스면 다시 만들지 않음
            world = cur if same else self.client.generate_opendrive_world(xodr, params)
        else:
            world = self.client.get_world()
            if not world.get_map().name.endswith(self.cfg["map"]):
                world = self.client.load_world(self.cfg["map"])
        world.wait_for_tick(60.0)
        self._clear_leftovers(world)
        self.tm = self.client.get_trafficmanager()
        self.tm_port = self.tm.get_port()
        s = world.get_settings()
        s.synchronous_mode, s.fixed_delta_seconds = True, DT        # 처음엔 60 FPS 목표 (self.dt 와 같음)
        world.apply_settings(s)
        self.tm.set_synchronous_mode(True)
        self.tm.set_random_device_seed(self.seed)
        self.tm.set_global_distance_to_leading_vehicle(self.cfg["gap"])
        self.tm.global_percentage_speed_difference(self.cfg["speed_diff"])
        self.tm.set_hybrid_physics_mode(True)
        self.tm.set_hybrid_physics_radius(120.0)
        world.set_weather(getattr(carla.WeatherParameters, self.cfg["weather"]))
        return world

    def _clear_leftovers(self, world):
        """이전 실행이 강제 종료돼 서버에 남은 이 시뮬레이터의 차(+붙어 있던 센서)를 지운다.
        그대로 두면 같은 자리에 차를 만들 때 'Spawn failed because of collision' 이 난다."""
        actors = world.get_actors()
        ours = {a.id for a in actors.filter("vehicle.*") if a.attributes.get("role_name") in ("hero", ROLE)}
        hidden = [o.id for o in _studio_objects(world)]     # 강제 종료로 정비소에서 숨긴 채 남은 구조물 되살림
        if hidden:
            world.enable_environment_objects(set(hidden), True)
        if ours:
            sensors = [a.id for a in actors.filter("sensor.*") if a.parent is not None and a.parent.id in ours]
            self.client.apply_batch_sync([carla.command.DestroyActor(x) for x in sensors + list(ours)])
            world.wait_for_tick(10.0)

    def _start_transform(self):
        if self.cfg["map"] == "lot":     # 주차장: 통로 입구
            wp = self.map.get_waypoint_xodr(0, -1, 8.0)
        elif self.cfg["map"] is None:     # 긴 직선: 시작점, 가운데 차로
            wp = self.map.get_waypoint_xodr(0, -max(1, (self.lanes + 1) // 2), 30.0)
        else:
            pts = self.map.get_spawn_points()
            return pts[self.rng.randrange(len(pts))]
        t = wp.transform
        t.location.z += 0.5
        return t

    # ------------------------------------------------------------------ 내 차
    def _blueprint(self, kind, powertrain):
        lib = self.world.get_blueprint_library()
        name = BLUEPRINTS.get((kind, powertrain), BLUEPRINTS[kind])
        found = lib.filter(name)
        if found:
            return found[0]
        base = {"car": "car", "bus": "bus", "truck": "truck"}[kind]
        any_kind = [b for b in lib.filter("vehicle.*") if b.has_attribute("base_type")
                    and b.get_attribute("base_type").as_str() == base]
        return any_kind[0] if any_kind else lib.filter("vehicle.*")[0]

    def _spawn_ego(self, kind, powertrain, transform):
        bp = self._blueprint(kind, powertrain)
        bp.set_attribute("role_name", "hero")
        paint = self._paint(kind, powertrain)
        if paint and bp.has_attribute("color"):    # 튜닝 단계별 도색 (외관)
            bp.set_attribute("color", paint)
        actor = self.world.try_spawn_actor(bp, transform)
        if actor is None:      # 자리가 막혀 있으면 살짝 위/앞에서 다시
            transform.location.z += 1.0
            actor = self.world.spawn_actor(bp, transform)
        self.ego = Ego(kind, powertrain, self.spec_for(kind, powertrain), actor)
        self.ego.paint = paint
        apply_specs(actor, self.ego.spec, powertrain)
        col = self.world.spawn_actor(self.world.get_blueprint_library().find("sensor.other.collision"),
                                     carla.Transform(), attach_to=actor)
        col.listen(self._on_collision)
        self._col_sensor, self._last_hit = col, -10.0
        if self.want_camera:
            self.set_view(self.view)

    def spec_for(self, kind, powertrain):
        """원래 제원 + 정비소 파츠 = 실제로 달리는 제원."""
        return apply_parts(self.specs[kind], self.parts.get(kind, {}), powertrain)

    def _paint(self, kind, powertrain):
        return PAINT.get(tier(self.parts.get(kind, {}), powertrain))

    def _on_collision(self, ev):
        if ev.timestamp - self._last_hit > 1.0:
            self.collisions += 1
        self._last_hit = ev.timestamp

    def set_vehicle(self, kind=None, powertrain=None):
        """설정 메뉴에서 호출: 같은 자리에서 차종/파워트레인/제원을 바꾼다."""
        e = self.ego
        kind, powertrain = kind or e.kind, powertrain or e.powertrain
        new_pt = powertrain != e.powertrain
        if self._blueprint(kind, powertrain).id == e.actor.type_id and self._paint(kind, powertrain) == e.paint:
            e.kind, e.powertrain, e.spec = kind, powertrain, self.spec_for(kind, powertrain)
            apply_specs(e.actor, e.spec, powertrain)
        else:
            t = e.actor.get_transform()
            t.location.z += 0.3
            self._destroy_ego()
            self._spawn_ego(kind, powertrain, t)
            if self.mode == "auto":
                self._apply_auto()
        if new_pt:
            self.soc = self._full_soc()

    def cycle_vehicle(self):
        kinds = ("car", "bus", "truck")
        self.set_vehicle(kind=kinds[(kinds.index(self.ego.kind) + 1) % 3])

    def cycle_powertrain(self):
        pts = ("auto", "manual", "ev", "hybrid")
        self.set_vehicle(powertrain=pts[(pts.index(self.ego.powertrain) + 1) % 4])

    def toggle_reverse(self):
        if self.speed() < 1.0:
            self.reverse = not self.reverse

    def shift(self, d):
        if self.ego.powertrain == "manual":
            self.gear = max(1, min(len(self.ego.spec["gears"]), self.gear + d))

    def reset_ego(self):
        """가까운 차로 가운데에 똑바로 세운다 (그 자리에 다른 차가 있으면 같은 차로 앞쪽 빈 곳)."""
        t = self.map.get_waypoint(self.ego.actor.get_location()).transform
        t.location.z += 0.5
        self.ego.actor.set_transform(self._free_spot(t))
        self.ego.actor.set_target_velocity(carla.Vector3D())
        self.ego.actor.set_target_angular_velocity(carla.Vector3D())
        self._timer_reset = True

    def adapt_dt(self, fps, now):
        """화면 FPS 가 목표(1/dt)를 못 따라가면 한 스텝 시간을 그만큼 늘려 게임이 슬로모션이 되지 않게 하고,
        여유가 생기면 10초마다 5 FPS 씩 다시 올려 본다 (최대 60, 최소 20)."""
        cap = round(1 / self.dt)
        if fps < cap * 0.93:
            new = max(20, int(fps))
        elif fps > cap * 0.98 and cap < 60 and now - self._dt_changed > 10.0:
            new = min(60, cap + 5)
        else:
            return
        self.dt, self._dt_changed = 1.0 / new, now
        s = self.world.get_settings()
        s.fixed_delta_seconds = self.dt
        self.world.apply_settings(s)

    def _check_flip(self):
        """뒤집히거나 옆으로 누운 채 2초가 지나면 근처 도로에 다시 세운다."""
        r = self.ego.actor.get_transform().rotation
        self._flip_t = self._flip_t + self.dt if abs(r.roll) > 60 or abs(r.pitch) > 60 else 0.0
        if self._flip_t > 2.0:
            self._flip_t = 0.0
            self.reset_ego()
            self.flip_notice = self.time + 3.0

    def _destroy_ego(self):
        for a in (self.camera, self._col_sensor):
            if a is not None:
                a.stop()
                a.destroy()
        self.camera = None
        self.ego.actor.destroy()

    # ------------------------------------------------------------------ 카메라
    def set_view(self, view):
        """cockpit(운전석) / chase(뒤에서) / top(위에서) 카메라를 내 차에 붙인다."""
        self.view = view
        if self.camera is not None:
            self.camera.stop()
            self.camera.destroy()
        spec, ext = self.ego.spec, self.ego.actor.bounding_box.extent
        if view == "cockpit":
            tr = carla.Transform(carla.Location(x=ext.x * (0.05 if self.ego.kind == "car" else 0.85),
                                                y=-0.35 if self.ego.kind == "car" else -0.6,
                                                z=spec["eye_height"]), carla.Rotation(pitch=-3))
        elif view == "showroom":           # 정비소: 차 둘레를 도는 카메라 (orbit() 이 위치를 정함)
            tr = carla.Transform(carla.Location(x=-ext.x * 2.6, z=ext.z * 2), carla.Rotation(pitch=-10))
        elif view == "chase":
            tr = carla.Transform(carla.Location(x=-ext.x - 6.0, z=ext.z * 2 + 2.0), carla.Rotation(pitch=-12))
        else:
            tr = carla.Transform(carla.Location(z=45.0), carla.Rotation(pitch=-90))
        bp = self.world.get_blueprint_library().find("sensor.camera.rgb")
        bp.set_attribute("image_size_x", str(self.frame_size[0]))
        bp.set_attribute("image_size_y", str(self.frame_size[1]))
        bp.set_attribute("fov", {"cockpit": "80", "showroom": "60"}.get(view, "90"))
        if view == "showroom":                       # 정비소: 빛 번짐·렌즈 플레어 끔 (차가 하얗게 묻히지 않게)
            for k in ("bloom_intensity", "lens_flare_intensity"):
                if bp.has_attribute(k):
                    bp.set_attribute(k, "0.0")
        self._cam_base = tr
        self.camera = self.world.spawn_actor(bp, tr, attach_to=self.ego.actor)
        self._frames = queue.Queue()
        self.camera.listen(self._frames.put)

    # ------------------------------------------------------------------ 정비소 (차를 공중 작업대로)
    def enter_studio(self):
        """정비소: 차를 갓길(있으면)에 세우고, 둘레의 가로등·표지판·가드레일·나무 등을 잠시 숨긴다.
        차로에 세워 두면 뒤에 차가 줄을 서고, 주변 구조물이 카메라를 가리기 때문. leave_studio() 가 되돌린다."""
        if self._studio is not None:
            return
        a = self.ego.actor
        self._studio = a.get_transform()
        a.set_target_velocity(carla.Vector3D())
        here = self._studio.location
        sh = self.map.get_waypoint(here, project_to_road=True, lane_type=carla.LaneType.Shoulder)
        if sh is not None and sh.transform.location.distance(here) < 12.0:
            t = sh.transform
            t.location.z += 0.3
            a.set_transform(t)
            here = t.location
        ext = a.bounding_box.extent
        R = max(ext.x, ext.y) * 5.0 + 8.0               # 카메라가 도는 거리 + 여유
        ids = [o.id for o in _studio_objects(self.world)
               if o.bounding_box.location.distance(here) - max(o.bounding_box.extent.x, o.bounding_box.extent.y) < R]
        self._hidden = ids
        if ids:
            self.world.enable_environment_objects(set(ids), False)

    def leave_studio(self):
        if self._studio is None:
            return
        if self._hidden:
            self.world.enable_environment_objects(set(self._hidden), True)
        self._hidden = []
        back = self._studio
        wp = self.map.get_waypoint(back.location)          # 출발은 차로 가운데·차로 방향으로 (가드레일에 붙어 있었어도)
        if wp is not None:
            back = wp.transform
            back.location.z += 0.3
        back, self._studio = self._free_spot(back), None
        a = self.ego.actor
        a.set_transform(back)
        a.set_target_velocity(carla.Vector3D())

    def _free_spot(self, t):
        """원래 자리에 다른 차가 와 있으면 같은 차로 앞쪽 빈 곳."""
        others = [v.get_location() for v in self.traffic if v.is_alive]
        clear = lambda loc: all(o.distance(loc) > 8.0 for o in others)  # noqa: E731
        if clear(t.location):
            return t
        wp = self.map.get_waypoint(t.location)
        for k in range(1, 30):
            nxt = wp.next(8.0 * k) if wp is not None else []
            if nxt and clear(nxt[0].transform.location):
                c = nxt[0].transform
                c.location.z += 0.3
                return c
        return t

    def orbit(self, angle_deg):
        """정비소 카메라를 차 둘레 angle_deg 위치로 (0 = 차 뒤, 90 = 왼쪽). 차 크기에 맞춰 거리를 잡는다."""
        if self.camera is None or self.view != "showroom":
            return
        ext = self.ego.actor.bounding_box.extent
        # 시야각 60° 에서 차 길이가 가로 약 22° 안에 들도록 (정비소 화면 가운데 칸 = 왼쪽·오른쪽 판 사이)
        r = max(ext.x, ext.y) * 5.0 + 1.0
        z = ext.z * 2.0 + 1.0
        a = math.radians(angle_deg + 180)
        loc = carla.Location(x=r * math.cos(a), y=r * math.sin(a), z=z)
        pitch = -(math.degrees(math.atan2(z - ext.z, r)) + 3.0)   # 차 중심을 살짝 위에 (아래는 파츠 카드 자리)
        self.camera.set_transform(carla.Transform(loc, carla.Rotation(pitch=pitch, yaw=angle_deg)))

    def _grab_frame(self, frame_id):
        while True:
            try:
                img = self._frames.get(timeout=2.0)
            except queue.Empty:
                return
            if img.frame >= frame_id:
                a = np.frombuffer(img.raw_data, np.uint8).reshape(img.height, img.width, 4)
                self.frame = a[:, :, 2::-1].copy()        # BGRA → RGB
                return

    # ------------------------------------------------------------------ 트래픽
    def _spawn_traffic(self):
        if self.n_cars <= 0:
            return
        lib = [b for b in self.world.get_blueprint_library().filter("vehicle.*")
               if b.get_attribute("number_of_wheels").as_int() == 4]
        if self.cfg["map"] is None:   # 긴 직선: 앞쪽 2 km 에 차로별로 흩어 배치
            ego_s = 30.0
            spots = [self.map.get_waypoint_xodr(0, -(1 + k % self.lanes), ego_s + 60 + 40 * k).transform
                     for k in range(self.n_cars) if ego_s + 60 + 40 * k < LONG_LENGTH - 100]
        else:
            ego_loc = self.ego.actor.get_location()
            spots = [p for p in self.map.get_spawn_points() if p.location.distance(ego_loc) > 15]
            self.rng.shuffle(spots)
        batch = []
        for t in spots[:self.n_cars]:
            t.location.z += 0.5
            bp = self.rng.choice(lib)
            bp.set_attribute("role_name", ROLE)
            if bp.has_attribute("color"):
                bp.set_attribute("color", self.rng.choice(bp.get_attribute("color").recommended_values))
            batch.append(carla.command.SpawnActor(bp, t).then(
                carla.command.SetAutopilot(carla.command.FutureActor, True, self.tm_port)))
        for r in self.client.apply_batch_sync(batch, True):
            if not r.error:
                self.traffic.append(self.world.get_actor(r.actor_id))
        for v in self.traffic:
            self.tm.random_left_lanechange_percentage(v, self.cfg["lane_change"])
            self.tm.random_right_lanechange_percentage(v, self.cfg["lane_change"])
            if self.cfg["map"] is None:
                self.tm.set_desired_speed(v, self.rng.uniform(100, 160))

    # ------------------------------------------------------------------ 주차장 (직각 + 평행)
    def _setup_parking(self):
        """생성한 주차장 맵에 주차된 차를 채우고 빈칸 몇 개를 목표로 남긴다. N = 다음 빈칸."""
        L = self.lot
        lib = [b for b in self.world.get_blueprint_library().filter("vehicle.*")
               if b.has_attribute("base_type") and b.get_attribute("base_type").as_str() == "car"]
        y_par = L["aisle"] + L["par_w"] / 2           # 오른쪽 평행 주차 줄 중심 (CARLA y, 오른쪽 +)
        y_perp = -(L["aisle"] + L["perp_d"] / 2)      # 왼쪽 직각 주차 줄 중심
        dbg = self.world.debug
        white = carla.Color(230, 230, 230)

        def line(x0, y0, x1, y1, col=white):
            dbg.draw_line(carla.Location(x0, y0, 0.05), carla.Location(x1, y1, 0.05), thickness=0.12,
                          color=col, life_time=0.0, persistent_lines=True)

        def park(x, y, yaw):
            bp = self.rng.choice(lib)
            bp.set_attribute("role_name", ROLE)
            a = self.world.try_spawn_actor(bp, carla.Transform(carla.Location(x, y, 0.3), carla.Rotation(yaw=yaw)))
            if a is not None:
                a.set_simulate_physics(False)
                self.parked.append(a)

        empty_perp, empty_par = {3, 8, 12}, {2, 5}
        x0 = 30.0
        for k in range(16):                           # 직각 주차칸 (앞머리를 바깥쪽으로)
            xc = x0 + (k + 0.5) * L["perp_w"]
            line(x0 + k * L["perp_w"], -L["aisle"], x0 + k * L["perp_w"], -(L["aisle"] + L["perp_d"]))
            if k in empty_perp:
                self.parking_targets.append(("perpendicular", carla.Transform(
                    carla.Location(xc, y_perp, 0.0), carla.Rotation(yaw=-90)), L["perp_d"], L["perp_w"]))
            else:
                park(xc, y_perp, -90)
        line(x0 + 16 * L["perp_w"], -L["aisle"], x0 + 16 * L["perp_w"], -(L["aisle"] + L["perp_d"]))
        for k in range(8):                            # 평행 주차칸
            xs = x0 + k * L["par_l"]
            line(xs, L["aisle"], xs, L["aisle"] + L["par_w"])
            xc = xs + L["par_l"] / 2
            if k in empty_par:
                self.parking_targets.append(("parallel", carla.Transform(
                    carla.Location(xc, y_par, 0.0), carla.Rotation(yaw=0)), L["par_l"], L["par_w"]))
            else:
                park(xc, y_par, 0)
        self.set_parking_target(0)

    def set_parking_target(self, i):
        if not self.parking_targets:
            return
        self.parking_i = i % len(self.parking_targets)
        kind, t, length, width = self.parking_targets[self.parking_i]
        self.parking_target = (t, length)
        f, r = _yaw_vectors(t.rotation.yaw)
        c = t.location
        pts = [carla.Location(c.x + f[0] * a * length / 2 + r[0] * b * width / 2,
                              c.y + f[1] * a * length / 2 + r[1] * b * width / 2, 0.08)
               for a, b in ((1, 1), (1, -1), (-1, -1), (-1, 1), (1, 1))]
        for p0, p1 in zip(pts, pts[1:]):              # 목표 칸을 초록색으로 (10분 유지)
            self.world.debug.draw_line(p0, p1, thickness=0.15, color=carla.Color(40, 220, 60), life_time=600.0)
        self.dest_name = f"Parking space {self.parking_i + 1}/{len(self.parking_targets)} ({kind})"

    def parking_status(self):
        if self.parking_target is None:
            return None
        t, space = self.parking_target
        f, r = _yaw_vectors(t.rotation.yaw)
        d = self.ego.actor.get_location() - t.location
        along, lat = d.x * f[0] + d.y * f[1], d.x * r[0] + d.y * r[1]
        yaw_err = abs((self.ego.actor.get_transform().rotation.yaw - t.rotation.yaw + 90) % 180 - 90)
        ok = abs(along) < (space - self.ego.actor.bounding_box.extent.x * 2) / 2 + 0.3 and abs(lat) < 0.5 \
            and yaw_err < 8 and self.speed() < 0.3
        kind = self.parking_targets[self.parking_i][0] if self.parking_targets else "parallel"
        return dict(target_along=along, target_lateral=-lat, yaw_error_deg=yaw_err, parked=ok, space_length=space,
                    kind=kind)

    # ------------------------------------------------------------------ 내비게이션
    def _destinations(self):
        if self.cfg["map"] == "lot":
            return [("Lot exit", self.map.get_waypoint_xodr(0, -1, 150.0).transform.location)]
        if self.cfg["map"] is None:
            wp = self.map.get_waypoint_xodr(0, -self.lanes, LONG_LENGTH - 300)
            return [("End of straight", wp.transform.location)]
        pts = sorted(self.map.get_spawn_points(), key=lambda p: (p.location.x, p.location.y))
        here = self.ego.actor.get_location()
        far = sorted(pts, key=lambda p: -p.location.distance(here))[: max(5, len(pts) // 3)]
        pick = far[:: max(1, len(far) // 5)][:5]
        return [(f"P{k + 1} ({p.location.distance(here) / 1000:.1f} km)", p.location) for k, p in enumerate(pick)]

    def set_destination(self, i):
        self.dest_i = i % len(self.dests)
        self.dest_name, loc = self.dests[self.dest_i]
        self.route = self.planner.trace(self.ego.actor.get_location(), loc)
        self.route_idx = 0
        if self.mode == "auto":
            self._apply_auto()

    def next_destination(self):
        if self.parking_targets:
            self.set_parking_target(self.parking_i + 1)
            return
        self.set_destination(self.dest_i + 1)

    def _track_route(self):
        if not self.route:
            return
        loc = self.ego.actor.get_location()
        lo, hi = self.route_idx, min(len(self.route), self.route_idx + 60)
        k = min(range(lo, hi), key=lambda j: self.route[j][0].transform.location.distance(loc))
        if self.route[k][0].transform.location.distance(loc) > 30:     # 경로를 벗어나면 다시 탐색
            self.set_destination(self.dest_i)
            return
        self.route_idx = k

    def guidance(self):
        name = tr(self.dest_name) if self.dest_name else self.dest_name      # 화면 언어로 (설정 메뉴에서 바뀜)
        if self.parking_targets:
            ok = self.parking_status()["parked"]
            name = tr("Parking space {i}/{n} ({kind})", i=self.parking_i + 1, n=len(self.parking_targets),
                      kind=tr(self.parking_targets[self.parking_i][0]))
            return dict(active=True, destination=name, distance=None, distance_to_exit=None,
                        instruction=(tr("Parked! N = next space") if ok else tr("Park in: {dest}", dest=name)),
                        arrow="arrive" if ok else None, arrived=ok, required_lane=None)
        return guidance(self.route, self.route_idx, name)

    # ------------------------------------------------------------------ 모드 / 한 스텝
    def cycle_mode(self):
        modes = ("manual", "acc", "auto")
        self.mode = modes[(modes.index(self.mode) + 1) % 3]
        self._apply_auto()

    def _apply_auto(self):
        a = self.ego.actor
        on = self.mode == "auto"
        a.set_autopilot(on, self.tm_port)
        if not on:
            return
        self.tm.auto_lane_change(a, True)
        self.tm.ignore_lights_percentage(a, 0)
        if self.set_speed is None and self.cfg["map"] is None:
            self.set_speed = 300 / 3.6           # 긴 직선 AUTO 기본 300 km/h
        if self.set_speed is not None:
            self.tm.set_desired_speed(a, self.set_speed * 3.6)
        if self.route:
            self.tm.set_path(a, [w.transform.location for w, _ in self.route[self.route_idx::10]])

    def change_set_speed(self, d_kmh):
        cur = self.set_speed or self.ego.actor.get_speed_limit() / 3.6
        self.set_speed = min(400 / 3.6, max(10 / 3.6, cur + d_kmh / 3.6))
        if self.mode == "auto":
            self.tm.set_desired_speed(self.ego.actor, self.set_speed * 3.6)

    def _full_soc(self):
        return {"ev": 0.9, "hybrid": 0.6}.get(self.ego.powertrain, 1.0)

    def _lead(self):
        """같은 차로 앞차 (간격 m, 속도 m/s). 없으면 (inf, 0)."""
        a = self.ego.actor
        tr = a.get_transform()
        f, r = _yaw_vectors(tr.rotation.yaw)
        half = self.map.get_waypoint(tr.location).lane_width / 2
        best = (math.inf, 0.0)
        for v in self.traffic + self.parked:
            d = v.get_location() - tr.location
            lx, ly = d.x * f[0] + d.y * f[1], d.x * r[0] + d.y * r[1]
            if 0 < lx < 200 and abs(ly) < half:
                gap = lx - v.bounding_box.extent.x - a.bounding_box.extent.x
                if gap < best[0]:
                    vel = v.get_velocity()
                    best = (gap, math.hypot(vel.x, vel.y))
        return best

    def speed(self):
        v = self.ego.actor.get_velocity()
        return math.hypot(v.x, v.y)

    def step(self, throttle=0.0, brake=0.0, steer=0.0):
        """steer: +는 왼쪽 (기존 시뮬레이터와 같음). AUTO 모드에서는 입력 무시, ACC 는 조향만 사람이."""
        a, spec, pt = self.ego.actor, self.ego.spec, self.ego.powertrain
        v = self.speed()
        if self.mode == "acc":                       # 속도·차간거리 자동 (IDM), 브레이크를 밟으면 즉시 사람 우선
            gap, lead_v = self._lead()
            target = self.set_speed or max(a.get_speed_limit() / 3.6, 5.0)
            acc = float(idm_accel(v, target, gap, lead_v, 2.0, 3.0, 1.5, 4.0))
            if brake < 0.05:
                throttle, brake = max(0.0, acc / 2.5), max(0.0, -acc / 7.0)
        if self.mode != "auto":
            lim = spec.get("speed_limiter_kmh", 0)
            if lim and v * 3.6 > lim:
                throttle = 0.0
            if pt == "ev" and self.soc <= 0.0:
                throttle = 0.0
            if pt in ("ev", "hybrid") and throttle < 0.02 and v > 2.0 and not self.reverse:   # 회생 (엔진브레이크처럼)
                regen = spec[pt]["coast_regen_n"] / spec["brake_force_n"]
                brake = min(1.0, brake + regen)
            c = carla.VehicleControl(throttle=float(throttle), brake=float(brake), steer=float(-steer),
                                     reverse=self.reverse)
            if pt == "manual":
                c.manual_gear_shift, c.gear = True, (-1 if self.reverse else self.gear)
            a.apply_control(c)
        if self.camera is not None and self.view == "cockpit":
            self._camera_fx(steer)
        frame_id = self.world.tick()
        self.time += self.dt
        self.odometer += v * self.dt
        creeping = a.get_control().throttle < 0.05 and v * 3.6 < 3.0   # 페달 없이 살살 구르는 정도면 아직 정지로 봄
        self.accel_timer.update(0.0 if self._timer_reset or creeping else v * 3.6, self.time)
        self._timer_reset = False
        self._update_vehicle_state(v)
        self._check_flip()
        self._track_route()
        if self.camera is not None:
            self._grab_frame(frame_id)

    def _update_vehicle_state(self, v_before):
        """RPM, 가속도, 미끄러짐, 출력[kW], 배터리 잔량(EV/하이브리드)."""
        a, spec, pt = self.ego.actor, self.ego.spec, self.ego.powertrain
        v = self.speed()
        self.ax = (v - v_before) / self.dt
        vel, tr = a.get_velocity(), a.get_transform()
        f, r = _yaw_vectors(tr.rotation.yaw)
        self.vy = vel.x * r[0] + vel.y * r[1]
        self.sliding = v > 3 and abs(self.vy) > 1.5
        w_wheel = v / spec["wheel_radius"]
        if pt == "ev":
            self.rpm = w_wheel * spec["ev"]["reduction"] * 60 / (2 * math.pi)
        else:
            g = a.get_control().gear
            if g >= 1:                              # 변속 중(0 = 중립)에는 직전 기어로 계산 (rpm 이 튀지 않게)
                self._last_gear = g
            g = min(self._last_gear, len(spec["gears"]))
            self.rpm = max(spec["idle_rpm"], w_wheel * spec["gears"][g - 1] * spec["final_drive"] * 60 / (2 * math.pi))
        p = (spec["mass"] * self.ax + 0.6 * spec["cd_a"] * v * v + spec["c_roll"] * spec["mass"] * 9.81) * v
        self.power_kw = p / 1000
        if pt in ("ev", "hybrid"):
            e = spec[pt]
            cap = e["battery_kwh"] * 3.6e6
            if p > 0:
                share = 1.0 if pt == "ev" else (1.0 if v * 3.6 < e["ev_only_below_kmh"] else 0.3)
                self.soc -= p * share / 0.9 * self.dt / cap
            else:
                self.soc += min(-p, e["regen_force_n"] * v) * 0.7 * self.dt / cap
            if pt == "hybrid" and self.soc < 0.3:       # 엔진이 발전해서 충전
                self.soc += 8000 * self.dt / cap
            self.soc = min(1.0, max(0.0, self.soc))

    def _camera_fx(self, steer):
        """운전석 카메라 연출: 가감속 피칭, 코너 롤, 코너 안쪽 바라보기, 속도에 따른 미세 진동."""
        fx = self._fx
        k = 1 - math.exp(-self.dt / 0.25)
        v = self.speed()
        ay = v * math.radians(self.ego.actor.get_angular_velocity().z)
        fx["ax"] += (max(-9, min(9, self.ax)) - fx["ax"]) * k
        fx["ay"] += (max(-9, min(9, ay)) - fx["ay"]) * k
        fx["look"] += (max(-8, min(8, steer * 6 * min(1, v / 10))) - fx["look"]) * (1 - math.exp(-self.dt / 0.4))
        fx["t"] += self.dt
        base = self._cam_base
        if not self.fx_enabled:
            self.camera.set_transform(base)
            return
        shake = min(1.0, v / 40) * 0.12
        rot = carla.Rotation(pitch=base.rotation.pitch - fx["ax"] * 0.35 + shake * math.sin(fx["t"] * 23),
                             yaw=base.rotation.yaw - fx["look"],
                             roll=base.rotation.roll + fx["ay"] * 0.4 + shake * math.sin(fx["t"] * 31))
        self.camera.set_transform(carla.Transform(base.location, rot))

    # ------------------------------------------------------------------ 관측값 (CUDA 내비 방식)
    def observe(self, preview=100.0, step=5.0, radius=150.0):
        """좌표는 모두 내 차 기준 (x: 전방 +, y: 왼쪽 +) [m]. 키 이름은 기존 env.py 와 최대한 같게."""
        a = self.ego.actor
        tr = a.get_transform()
        f, r = _yaw_vectors(tr.rotation.yaw)
        o = tr.location

        def local(locs):
            out = np.zeros((len(locs), 2), np.float32)
            for i, p in enumerate(locs):
                dx, dy = p.x - o.x, p.y - o.y
                out[i] = (dx * f[0] + dy * f[1], -(dx * r[0] + dy * r[1]))
            return out

        wp = self.map.get_waypoint(o)
        n = max(1, int(preview / step))
        centre, w = [], wp
        for _ in range(n):
            centre.append(w)
            nxt = w.next(step)
            if not nxt:
                break
            w = nxt[0]

        def edge(side):
            pts = []
            for c in centre:
                cf, cr = _yaw_vectors(c.transform.rotation.yaw)
                k = c.lane_width / 2 * (1 if side == "right" else -1)
                p = c.transform.location
                pts.append(carla.Location(p.x + cr[0] * k, p.y + cr[1] * k, p.z))
            return local(pts)

        lc = local([c.transform.location for c in centre])
        kappa = np.zeros(len(lc), np.float32)
        if len(lc) >= 3:
            h = np.unwrap(np.arctan2(np.diff(lc[:, 1]), np.diff(lc[:, 0])))
            kappa[1:-1] = np.diff(h) / step
        if self.route:
            route = local([w.transform.location for w, _ in self.route[self.route_idx::max(1, int(step / STEP))][:n]])
        else:
            route = lc
        # 같은 방향 차로 수 / 내 차로 번호 (0 = 오른쪽 끝)
        lane, x = 0, wp.get_right_lane()
        while _driving(x) and x.lane_id * wp.lane_id > 0:
            lane, x = lane + 1, x.get_right_lane()
        n_lanes, x = lane + 1, wp.get_left_lane()
        while _driving(x) and x.lane_id * wp.lane_id > 0:
            n_lanes, x = n_lanes + 1, x.get_left_lane()
        d = o - wp.transform.location
        wf, wr = _yaw_vectors(wp.transform.rotation.yaw)
        others, lead_gap, lead_v = [], np.inf, 0.0
        for v in self.traffic + self.parked:
            p = v.get_location()
            if p.distance(o) > radius:
                continue
            (lx, ly), = local([p])
            vel = v.get_velocity()
            ext = v.bounding_box.extent
            others.append((lx, ly, math.hypot(vel.x, vel.y), self.map.get_waypoint(p).lane_id,
                           ext.x * 2, ext.y * 2))
            if lx > 0 and abs(ly) < wp.lane_width / 2:
                gap = lx - ext.x - a.bounding_box.extent.x
                if gap < lead_gap:
                    lead_gap, lead_v = gap, math.hypot(vel.x, vel.y)
        limit = a.get_speed_limit() / 3.6
        ctl = a.get_control()
        max_steer = math.radians(self.ego.spec["max_steer_deg"])
        obs = dict(
            speed=np.float32(self.speed() * (-1 if self.reverse else 1)),
            steer_angle=np.float32(-ctl.steer * max_steer), max_steer=np.float32(max_steer),
            lane=lane, lane_id=wp.lane_id, n_lanes=n_lanes, lane_width=wp.lane_width,
            lateral_offset=np.float32(-(d.x * wr[0] + d.y * wr[1])),
            on_ramp=False,
            heading_error=np.float32(-math.radians((tr.rotation.yaw - wp.transform.rotation.yaw + 180) % 360 - 180)),
            speed_limit=np.float32(limit),
            recommended_speed=np.float32(min(limit, math.sqrt(2.5 / max(float(np.abs(kappa).max()), 1e-4)))),
            lane_center=lc, lane_left=edge("left"), lane_right=edge("right"),
            route=route, nav=self.guidance(), curvature=kappa,
            lead_gap=np.float32(lead_gap), lead_speed=np.float32(lead_v),
            vehicles=np.array(others, np.float32).reshape(-1, 6),   # x, y, speed, lane_id, length, width
            gear=ctl.gear, reverse=self.reverse, powertrain=self.ego.powertrain, vehicle_kind=self.ego.kind,
            rpm=np.float32(self.rpm), power_kw=np.float32(self.power_kw), battery_soc=np.float32(self.soc),
            collisions=self.collisions, time=np.float32(self.time),
        )
        if self.parking_target is not None:
            t, _ = self.parking_target
            obs["parking"] = self.parking_status()
            obs["parking_target"] = local([t.location])[0]
        return obs

    # ------------------------------------------------------------------ 정리
    def destroy(self):
        if self._hidden:                              # 정비소에서 숨긴 구조물 되살림
            self.world.enable_environment_objects(set(self._hidden), True)
        self.tm.set_synchronous_mode(False)
        s = self.world.get_settings()
        s.synchronous_mode, s.fixed_delta_seconds = False, None
        self.world.apply_settings(s)
        self._destroy_ego()
        self.client.apply_batch([carla.command.DestroyActor(x) for x in self.traffic + self.parked])


def _studio_objects(world):
    """정비소 카메라를 가릴 수 있는 맵 구조물 (가로등·표지판·신호등·가드레일·울타리·벽·나무·기타 고정물)."""
    out = []
    for name in ("Poles", "TrafficSigns", "TrafficLight", "GuardRail", "Fences", "Walls", "Vegetation", "Static"):
        label = getattr(carla.CityObjectLabel, name, None)
        if label is not None:
            out += world.get_environment_objects(label)
    return out


def _driving(wp):
    return wp is not None and str(wp.lane_type) == "Driving"
