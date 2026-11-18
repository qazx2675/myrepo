"""CARLA 월드 래퍼: 맵/트래픽/내 차/카메라/내비/주차/제로백을 한 곳에서 관리.

CARLA 서버(CarlaUE4.exe)가 먼저 떠 있어야 한다. 동기 모드(고정 1/30초)로 돌려서
step() 한 번 = 물리 한 스텝 = 카메라 한 프레임이 정확히 맞는다 (CUDA 학습/평가에 필요).
"""
import math
import queue
import random

import carla
import numpy as np

from carla_sim.physics import apply_specs
from carla_sim.route import STEP, RoutePlanner, guidance
from carla_sim.xodr import long_straight_xodr
from sim.vehicle import AccelTimer

DT = 1.0 / 30.0

# 난이도/코스 → 맵, 트래픽, Traffic Manager 성향, 날씨
LEVELS = {
    "easy":    dict(map="Town04", cars=25, speed_diff=20, gap=4.0, lane_change=0, weather="ClearNoon"),
    "normal":  dict(map="Town04", cars=70, speed_diff=0, gap=2.5, lane_change=5, weather="CloudyNoon"),
    "hard":    dict(map="Town05", cars=110, speed_diff=-20, gap=1.2, lane_change=20, weather="WetCloudySunset"),
    "long":    dict(map=None, cars=20, speed_diff=0, gap=3.0, lane_change=3, weather="ClearNoon"),
    "parking": dict(map="Town10HD_Opt", cars=0, speed_diff=0, gap=3.0, lane_change=0, weather="ClearNoon"),
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
                 view="cockpit", frame_size=(1280, 720), camera=True):
        self.client, self.level_key, self.specs = client, level, specs
        self.cfg = LEVELS[level]
        self.n_cars = self.cfg["cars"] if cars is None else cars
        self.seed, self.lanes, self.frame_size, self.view = seed, lanes, frame_size, view
        self.rng = random.Random(seed)
        self.tm = client.get_trafficmanager()
        self.tm_port = self.tm.get_port()
        self.world = self._load_world()
        self.map = self.world.get_map()
        self.planner = RoutePlanner(self.map)
        self.traffic, self.parked = [], []
        self.camera, self.frame, self._frames = None, None, queue.Queue()
        self.want_camera = camera
        self.mode = "manual"                # manual / auto (Traffic Manager 가 운전)
        self.gear, self.reverse = 1, False
        self.set_speed = None               # AUTO 목표 속도 [m/s]
        self.collisions, self.time, self.odometer = 0, 0.0, 0.0
        self.accel_timer = AccelTimer()
        self.route, self.route_idx, self.dest_name, self.dest_i = [], 0, None, -1
        self.parking_target = None
        self.mode_label = None
        self.ego = None
        self._spawn_ego(vehicle, powertrain, self._start_transform())
        if level == "parking":
            self._setup_parking()
        self._spawn_traffic()
        self.dests = self._destinations()
        self.world.tick()

    # ------------------------------------------------------------------ 맵/월드
    def _load_world(self):
        if self.cfg["map"] is None:
            params = carla.OpendriveGenerationParameters(
                vertex_distance=2.0, max_road_length=500.0, wall_height=1.0, additional_width=0.6,
                smooth_junctions=True, enable_mesh_visibility=True)
            world = self.client.generate_opendrive_world(long_straight_xodr(LONG_LENGTH, self.lanes), params)
        else:
            world = self.client.get_world()
            if not world.get_map().name.endswith(self.cfg["map"]):
                world = self.client.load_world(self.cfg["map"])
        s = world.get_settings()
        s.synchronous_mode, s.fixed_delta_seconds = True, DT
        world.apply_settings(s)
        self.tm.set_synchronous_mode(True)
        self.tm.set_random_device_seed(self.seed)
        self.tm.set_global_distance_to_leading_vehicle(self.cfg["gap"])
        self.tm.global_percentage_speed_difference(self.cfg["speed_diff"])
        self.tm.set_hybrid_physics_mode(True)
        self.tm.set_hybrid_physics_radius(120.0)
        world.set_weather(getattr(carla.WeatherParameters, self.cfg["weather"]))
        return world

    def _start_transform(self):
        if self.cfg["map"] is None:     # 긴 직선: 시작점, 가운데 차로
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
        actor = self.world.try_spawn_actor(bp, transform)
        if actor is None:      # 자리가 막혀 있으면 살짝 위/앞에서 다시
            transform.location.z += 1.0
            actor = self.world.spawn_actor(bp, transform)
        self.ego = Ego(kind, powertrain, self.specs[kind], actor)
        apply_specs(actor, self.ego.spec, powertrain)
        col = self.world.spawn_actor(self.world.get_blueprint_library().find("sensor.other.collision"),
                                     carla.Transform(), attach_to=actor)
        col.listen(self._on_collision)
        self._col_sensor, self._last_hit = col, -10.0
        if self.want_camera:
            self.set_view(self.view)

    def _on_collision(self, ev):
        if ev.timestamp - self._last_hit > 1.0:
            self.collisions += 1
        self._last_hit = ev.timestamp

    def set_vehicle(self, kind=None, powertrain=None):
        """설정 메뉴에서 호출: 같은 자리에서 차종/파워트레인/제원을 바꾼다."""
        e = self.ego
        kind, powertrain = kind or e.kind, powertrain or e.powertrain
        if self._blueprint(kind, powertrain).id == e.actor.type_id:
            e.kind, e.powertrain, e.spec = kind, powertrain, self.specs[kind]
            apply_specs(e.actor, e.spec, powertrain)
            return
        t = e.actor.get_transform()
        t.location.z += 0.3
        self._destroy_ego()
        self._spawn_ego(kind, powertrain, t)

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
        t = self.map.get_waypoint(self.ego.actor.get_location()).transform
        t.location.z += 0.5
        self.ego.actor.set_transform(t)
        self.ego.actor.set_target_velocity(carla.Vector3D())

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
        elif view == "chase":
            tr = carla.Transform(carla.Location(x=-ext.x - 6.0, z=ext.z * 2 + 2.0), carla.Rotation(pitch=-12))
        else:
            tr = carla.Transform(carla.Location(z=45.0), carla.Rotation(pitch=-90))
        bp = self.world.get_blueprint_library().find("sensor.camera.rgb")
        bp.set_attribute("image_size_x", str(self.frame_size[0]))
        bp.set_attribute("image_size_y", str(self.frame_size[1]))
        bp.set_attribute("fov", "90" if view != "cockpit" else "80")
        self.camera = self.world.spawn_actor(bp, tr, attach_to=self.ego.actor)
        self._frames = queue.Queue()
        self.camera.listen(self._frames.put)

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

    # ------------------------------------------------------------------ 주차 (평행 주차)
    def _setup_parking(self):
        """맨 오른쪽 차로 직선 구간에 주차된 차를 줄지어 세우고, 그 사이 빈칸을 목표로 한다."""
        lib = [b for b in self.world.get_blueprint_library().filter("vehicle.*")
               if b.has_attribute("base_type") and b.get_attribute("base_type").as_str() == "car"]
        base = None
        for p in self.map.get_spawn_points():
            wp = self.map.get_waypoint(p.location)
            while _driving(wp.get_right_lane()) and wp.get_right_lane().lane_id * wp.lane_id > 0:
                wp = wp.get_right_lane()
            ahead = wp.next(70.0)
            if ahead and not any(w.is_junction for w in [wp] + wp.next_until_lane_end(5.0)[:14]) \
                    and abs((ahead[0].transform.rotation.yaw - wp.transform.rotation.yaw + 180) % 360 - 180) < 3:
                base = wp
                break
        if base is None:
            return
        ego_len = self.ego.actor.bounding_box.extent.x * 2
        space = ego_len + 2.5
        s = 0.0
        for k in range(6):
            if k == 3:                                       # 4번째 자리를 비운다
                self.parking_target = (base.next(s + space / 2)[0].transform, space)
                s += space
                continue
            bp = self.rng.choice(lib)
            t = base.next(s + 2.4)[0].transform
            t.location.z += 0.3
            a = self.world.try_spawn_actor(bp, t)
            if a is not None:
                a.set_simulate_physics(False)
                self.parked.append(a)
            s += 5.6
        left = base.get_left_lane()
        start = (left if _driving(left) else base).next(s + 8.0)[0].transform
        start.location.z += 0.5
        self.ego.actor.set_transform(start)

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
        return dict(target_along=along, target_lateral=-lat, yaw_error_deg=yaw_err, parked=ok, space_length=space)

    # ------------------------------------------------------------------ 내비게이션
    def _destinations(self):
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
        return guidance(self.route, self.route_idx, self.dest_name)

    # ------------------------------------------------------------------ 모드 / 한 스텝
    def cycle_mode(self):
        self.mode = "auto" if self.mode == "manual" else "manual"
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

    def speed(self):
        v = self.ego.actor.get_velocity()
        return math.hypot(v.x, v.y)

    def step(self, throttle=0.0, brake=0.0, steer=0.0):
        """steer: +는 왼쪽 (기존 시뮬레이터와 같음). AUTO 모드에서는 입력 무시."""
        a, spec = self.ego.actor, self.ego.spec
        v = self.speed()
        if self.mode == "manual":
            lim = spec.get("speed_limiter_kmh", 0)
            if lim and v * 3.6 > lim:
                throttle = 0.0
            c = carla.VehicleControl(throttle=float(throttle), brake=float(brake), steer=float(-steer),
                                     reverse=self.reverse)
            if self.ego.powertrain == "manual":
                c.manual_gear_shift, c.gear = True, (-1 if self.reverse else self.gear)
            a.apply_control(c)
        frame_id = self.world.tick()
        self.time += DT
        self.odometer += v * DT
        self.accel_timer.update(v * 3.6, self.time)
        self._track_route()
        if self.camera is not None:
            self._grab_frame(frame_id)

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
            collisions=self.collisions, time=np.float32(self.time),
        )
        if self.parking_target is not None:
            t, _ = self.parking_target
            obs["parking"] = self.parking_status()
            obs["parking_target"] = local([t.location])[0]
        return obs

    # ------------------------------------------------------------------ 정리
    def destroy(self):
        self.tm.set_synchronous_mode(False)
        s = self.world.get_settings()
        s.synchronous_mode, s.fixed_delta_seconds = False, None
        self.world.apply_settings(s)
        self._destroy_ego()
        self.client.apply_batch([carla.command.DestroyActor(x) for x in self.traffic + self.parked])


def _driving(wp):
    return wp is not None and str(wp.lane_type) == "Driving"
