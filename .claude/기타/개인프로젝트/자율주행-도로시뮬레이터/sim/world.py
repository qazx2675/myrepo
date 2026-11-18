"""시뮬레이션 상태(World).

차량 상태는 SoA(Structure of Arrays) numpy 배열로 보관한다. 인덱스 0 = 내 차(ego),
1..N-1 = AI 트래픽. 나중에 CUDA로 옮길 때 이 배열들을 그대로 디바이스 메모리로 올리면 된다.

  s, d       : 본선 Frenet 위치 [m] (d 왼쪽 +). 내 차가 진출로에 있으면 본선 투영값
  v, accel   : 속도 [m/s], 가속도 [m/s^2]
  x, y, yaw  : 월드 좌표 (렌더링/센서용)
  lane       : AI 목표 차로 번호 (0 = 1차로)
  length, width, height, kind ("car" / "truck" / "bus")

내 차는 sim/vehicle.py 의 동역학 모델(엔진/변속기/전기모터, 타이어)로 움직이고,
AI 차량은 IDM(차간거리 유지) + MOBIL(차선 변경)로 움직인다.
"""
import math

import numpy as np

from .controllers import IDMController, idm_accel
from .nav import PREPARE_DIST, Navigator
from .scenario import get_level
from .track import Track
from .vehicle import POWERTRAINS, AccelTimer, Vehicle, load_specs

MODES = ("manual", "acc", "auto")   # 수동 / 종방향만 자동(ACC) / 완전 자동
KINDS = ("car", "bus", "truck")

CAR_COLORS = [(200, 30, 30), (230, 230, 235), (30, 30, 35), (40, 80, 170), (150, 150, 155),
              (20, 110, 60), (230, 180, 30), (90, 50, 30), (120, 160, 210)]
TRUCK_COLORS = [(235, 235, 235), (60, 90, 150), (180, 40, 40), (230, 140, 30)]
BUS_COLORS = [(40, 120, 200), (30, 150, 90), (220, 220, 220)]
AI_DIMS = {"car": (4.6, 1.85, 1.45), "truck": (12.0, 2.5, 3.6), "bus": (12.0, 2.5, 3.3)}
FAR = -1000.0   # 본선 밖(진출로 깊숙이)에 있는 내 차의 d 값


class World:
    def __init__(self, level="normal", vehicle="car", powertrain="auto", n_cars=None,
                 seed=0, n_lanes=3, track=None, ai_controller=None):
        self.level = get_level(level)
        lv = self.level
        self.track = t = Track(n_lanes=n_lanes, level=track or lv["track"])
        self.rng = rng = np.random.default_rng(seed)
        self.ai_controller = ai_controller or IDMController()
        self.specs = load_specs()
        n_ai = lv["n_cars"] if n_cars is None else n_cars
        if n_cars is None and track:      # 코스 길이에 맞춰 밀도 유지
            n_ai = int(round(n_ai * t.L / 4230.0))
        n = n_ai + 1
        self.n = n

        roll = rng.random(n)
        kind = np.where(roll < lv["truck_ratio"], "truck",
                        np.where(roll < lv["truck_ratio"] + lv["bus_ratio"], "bus", "car")).astype(object)
        kind[0] = vehicle
        self.kind = kind
        self.is_heavy = kind != "car"
        dims = np.array([AI_DIMS[k] for k in kind])
        self.length = dims[:, 0] * np.where(kind == "car", rng.uniform(0.93, 1.07, n), 1.0)
        self.width, self.height = dims[:, 1].copy(), dims[:, 2].copy()
        self.a_max = np.where(self.is_heavy, 0.8, rng.uniform(1.2, 1.8, n))
        self.b = np.full(n, 2.0)
        self.T = np.where(self.is_heavy, max(1.5, lv["headway"][1]), rng.uniform(*lv["headway"], n))
        self.s0 = np.full(n, 2.5)
        self.v_factor = np.where(self.is_heavy, 1.0, rng.uniform(0.9, 1.1, n))
        self.v_cap = np.where(kind == "truck", 25.0, np.where(kind == "bus", 27.8, 99.0))
        self.color = [TRUCK_COLORS[i % 4] if kind[i] == "truck" else BUS_COLORS[i % 3] if kind[i] == "bus"
                      else CAR_COLORS[rng.integers(len(CAR_COLORS))] for i in range(n)]
        self.color[0] = (250, 200, 40)

        # 초기 배치: 차로별로 고르게 + 랜덤 흔들림. 내 차 주변 60 m는 비움. 대형차는 1차로 금지.
        L, nl = t.L, t.n_lanes
        lane = rng.integers(0, nl, n)
        bad = self.is_heavy & (lane == 0)
        if nl > 1:
            lane[bad] = rng.integers(1, nl, int(bad.sum()))
        s = np.zeros(n)
        for l in range(nl):
            ids = np.where((lane == l) & (np.arange(n) > 0))[0]
            if len(ids):
                sp = (L - 120) / len(ids)
                s[ids] = np.linspace(60, L - 60, len(ids), endpoint=False) + rng.uniform(0, 0.4 * sp, len(ids))
        lane[0] = 1 if nl > 1 else 0
        self.lane, self.s = lane, s
        self.d = t.lane_d(lane).astype(float)
        self.v = np.minimum(t.vref_at(s) * self.v_factor, self.v_cap) * 0.9
        self.v[0] = 0.0
        self.accel = np.zeros(n)
        self.vlat = np.zeros(n)
        self.lc_timer = rng.uniform(0, 3, n)

        self.nav = Navigator(t)
        self.fog_dist = lv["fog_dist"]
        self.ego_mode = "manual"
        self.mode_label = None
        self.ego_set_speed = None    # ACC 설정속도 [m/s], None = 제한속도 따라감
        self.pedals = (0.0, 0.0)
        self.ego = None
        self._spawn_ego(vehicle, powertrain, 0.0, int(lane[0]), 0.0)

        self.x, self.y, self.yaw = (np.zeros(n) for _ in range(3))
        self.leader = np.full(n, -1)
        self.gap = np.full(n, np.inf)
        self.time = 0.0
        self.odometer = 0.0          # 내 차 누적 주행거리 [m]
        self.accel_timer = AccelTimer()
        self.collisions = 0
        self.rail_hits = 0
        self.lane_changes = 0
        self.forced_brake = None     # (차량 index, 끝나는 시각) - 급제동 이벤트
        self.next_brake_event = lv["brake_event_every"] or math.inf
        self._colliding = np.zeros(n - 1, bool)
        self._on_rail = False
        self._sync_ego_arrays()
        self._update_world_coords()

    # ------------------------------------------------------------------
    def _spawn_ego(self, kind, powertrain, s, lane, speed):
        t = self.track
        x, y, psi = t.to_world(s, t.lane_d(lane))
        e = Vehicle(self.specs[kind], kind, powertrain, float(x), float(y), float(psi))
        e.s, e.d = float(s) % t.L, float(t.lane_d(lane))
        e.vx = speed
        self.ego = e
        self.ego_s_main = e.s
        self._set_ego_dims()
        self.ego_target_lane = self.ego_lane_cmd = lane

    def _set_ego_dims(self):
        e = self.ego
        self.kind[0] = e.kind
        self.is_heavy[0] = e.kind != "car"
        self.length[0], self.width[0], self.height[0] = e.length, e.width, e.height

    def set_vehicle(self, kind=None, powertrain=None):
        """차종/파워트레인 변경 (현재 위치에서 이어서 주행)."""
        e = self.ego
        nv = Vehicle(self.specs[kind or e.kind], kind or e.kind, powertrain or e.powertrain, e.x, e.y, e.yaw)
        nv.s, nv.d, nv.road = e.s, e.d, e.road
        nv.vx = min(e.vx, 15.0)
        self.ego = nv
        self._set_ego_dims()

    def cycle_vehicle(self):
        self.set_vehicle(kind=KINDS[(KINDS.index(self.ego.kind) + 1) % len(KINDS)])

    def cycle_powertrain(self):
        pts = POWERTRAINS
        self.set_vehicle(powertrain=pts[(pts.index(self.ego.powertrain) + 1) % len(pts)])

    # ------------------------------------------------------------------
    def observation(self):
        """모든 차량의 상태 배열 (AI 컨트롤러 입력)."""
        t = self.track
        v0 = np.minimum(t.vref_at(self.s) * self.v_factor, self.v_cap)
        if self.ego_set_speed is not None:
            v0[0] = min(v0[0], self.ego_set_speed)
        if self.ego.spec["speed_limiter_kmh"]:
            v0[0] = min(v0[0], self.ego.spec["speed_limiter_kmh"] / 3.6)
        target_d = t.lane_d(self.lane).astype(float)
        target_d[0] = t.lane_d(self.ego_lane_cmd) if self.ego_mode == "auto" else self.d[0]
        return dict(s=self.s, d=self.d, v=self.v, target_d=target_d, v0=v0,
                    length=self.length, width=self.width, a_max=self.a_max, b=self.b,
                    T=self.T, s0=self.s0, track_length=t.L)

    def ego_road(self):
        return self.ego.road if self.ego.road is not None else self.track

    def ego_observation(self):
        """내 차 기준 관측값 (HUD 표시, 외부 컨트롤러용)."""
        t, e = self.track, self.ego
        road = self.ego_road()
        _, _, psi = road.pose(e.s)
        j = int(self.leader[0])
        return dict(
            v=e.v, s=e.s, d=e.d, on_ramp=e.road is not None,
            lane=int(t.lane_of(e.d)) if e.road is None else t.n_lanes - 1,
            steer=e.steer,
            heading_error=float((e.yaw - psi + math.pi) % (2 * math.pi) - math.pi),
            speed_limit_kmh=road.limit_at(e.s),
            kappa_preview=road.curvature_preview(e.s),
            lead_gap=float(self.gap[0]) if j >= 0 else None,
            lead_v=float(self.v[j]) if j >= 0 else None,
        )

    # ------------------------------------------------------------------
    def step(self, dt, throttle=0.0, brake=0.0, steer_in=0.0):
        t, e = self.track, self.ego
        obs = self.observation()
        acc, self.leader, self.gap = self.ai_controller(obs)
        acc = np.array(acc, dtype=float)
        self._brake_events(acc)

        # --- 내 차 ---
        if self.ego_mode in ("acc", "auto"):
            a_des = acc[0]
            if e.road is not None:   # 진출로: 제한속도 + 끝에서 정지
                v0 = min(e.road.limit_at(e.s) / 3.6, math.sqrt(2 * 1.2 * max(0.0, e.road.L - 12 - e.s)))
                a_des = -3.0 if v0 < 0.5 else float(idm_accel(e.vx, v0, np.inf, 0.0, 1.5, 2.0, 1.5, 2.5))
            thr, brk = e.pedals_for_accel(a_des)
            if brake:
                thr, brk = 0.0, 1.0
        else:
            thr, brk = throttle, brake
        if self.ego_mode == "auto":
            steer_t = self._autopilot_steer()
        else:
            steer_t = steer_in * e.max_steer()
        e.auto_shift = self.ego_mode != "manual"
        e.step(thr, brk, steer_t, dt, offroad=self._offroad())
        self.odometer += e.vx * dt
        self.accel_timer.update(e.vx * 3.6, self.time + dt)
        self.pedals = (thr, brk)

        # --- AI 차선 변경 (MOBIL) + 적분 ---
        self._lane_changes(acc, obs["v0"], dt)
        ai = slice(1, None)
        v_new = np.maximum(0.0, self.v[ai] + acc[ai] * dt)
        self.s[ai] = np.mod(self.s[ai] + 0.5 * (self.v[ai] + v_new) * dt, t.L)
        self.v[ai] = v_new
        td = t.lane_d(self.lane[ai])
        self.vlat[ai] = np.clip(0.9 * (td - self.d[ai]), -1.1, 1.1)
        self.d[ai] += self.vlat[ai] * dt
        acc[0] = e.ax
        self.accel = acc

        self._update_ego_road()
        self._sync_ego_arrays()
        self._check_collisions()
        self._update_world_coords()
        self.time += dt

    # ------------------------------------------------------------------
    def _autopilot_steer(self):
        t, e = self.track, self.ego
        if e.road is None:
            cur = int(t.lane_of(e.d))
            desired = self.ego_target_lane
            de = self.nav.dist_to_exit(e)
            if de is not None and de < PREPARE_DIST:
                desired = t.n_lanes - 1
            self.ego_lane_cmd = cur + int(np.sign(desired - cur))
        else:
            self.ego_lane_cmd = t.n_lanes - 1
        ld = 6.0 + 0.8 * e.vx + 0.5 * e.wheelbase
        x, y = self.nav.route_xy(e, [ld], self.ego_lane_cmd)
        dx, dy = float(x[0]) - e.x, float(y[0]) - e.y
        c, sn = math.cos(e.yaw), math.sin(e.yaw)
        lx, ly = dx * c + dy * sn, -dx * sn + dy * c
        alpha = math.atan2(ly, lx)
        return math.atan2(2.0 * e.wheelbase * math.sin(alpha), math.hypot(lx, ly))

    def _offroad(self):
        e = self.ego
        road = self.ego_road()
        return abs(e.d) > road.half_width + road.shoulder

    def _clamp(self, road, s, d):
        x, y, psi = road.to_world(s, d)
        self.ego.hit_rail(float(x), float(y), float(psi))
        self.ego.s, self.ego.d = s, d
        if not self._on_rail:
            self.rail_hits += 1
        self._on_rail = True

    def _update_ego_road(self):
        """내 차가 본선/진출로 중 어디에 있는지 판정 + 가드레일 처리."""
        t, e = self.track, self.ego
        rail_hit = False
        if e.road is None:
            e.s, e.d = t.project(e.x, e.y, s_hint=e.s)
            d_right = float(t.lane_d(t.n_lanes - 1))
            for r in t.ramps:
                u = (e.s - r.s0) % t.L
                if u <= r.taper + 10:
                    sr, dr = r.project(e.x, e.y, s_hint=u)
                    if e.d < d_right - 1.0 and abs(dr) < abs(e.d - d_right):
                        e.road, e.s, e.d = r, sr, dr
                        break
            if e.road is None:
                lim = t.rail_d - e.width / 2 - 0.2
                if e.d > lim:
                    self._clamp(t, e.s, lim)
                    rail_hit = True
                elif e.d < -lim and (not t.in_exit_zone(e.s) or e.d < -lim - 40):
                    self._clamp(t, e.s, -lim)
                    rail_hit = True
        else:
            r = e.road
            sr, dr = r.project(e.x, e.y, s_hint=e.s)
            if sr < r.taper and dr > r.lane_width * 0.5 + 0.5:
                e.road = None
                e.s, e.d = t.project(e.x, e.y, s_hint=r.s0 + sr)
            else:
                e.s, e.d = sr, dr
                lim = r.rail_d - e.width / 2
                if dr < -lim:
                    self._clamp(r, sr, -lim)
                    rail_hit = True
                elif dr > lim and sr > r.taper + 20:
                    self._clamp(r, sr, lim)
                    rail_hit = True
                if e.s >= r.L - 1.0:     # 진출로 끝 (목적지)
                    e.vx = e.vy = e.r = 0.0
        if not rail_hit:
            self._on_rail = False

    def _sync_ego_arrays(self):
        t, e = self.track, self.ego
        if e.road is None:
            self.ego_s_main = e.s
            self.s[0], self.d[0] = e.s, e.d
        else:
            sm, dm = t.project(e.x, e.y, s_hint=e.road.s0 + min(e.s, e.road.taper))
            self.ego_s_main = sm
            past = e.s > e.road.taper * 0.7      # 본선에서 충분히 벗어나면 AI 와 상호작용 없음
            self.s[0], self.d[0] = sm, (FAR if past or abs(dm) > t.half_width + 4 else dm)
        self.v[0] = e.vx

    # ------------------------------------------------------------------
    def _brake_events(self, acc):
        """시나리오의 '앞차 급제동' 이벤트: 내 차 앞 선행차가 몇 초간 강하게 제동."""
        if self.forced_brake is not None:
            i, until = self.forced_brake
            if self.time < until:
                acc[i] = min(acc[i], -6.0)
            else:
                self.forced_brake = None
        elif self.time >= self.next_brake_event:
            self.next_brake_event = self.time + self.level["brake_event_every"] * self.rng.uniform(0.7, 1.3)
            j = int(self.leader[0])
            if j > 0 and self.gap[0] < 150:
                self.forced_brake = (j, self.time + self.rng.uniform(1.5, 3.0))

    def _lane_changes(self, acc, v0, dt):
        t = self.track
        self.lc_timer -= dt
        for i in np.where(self.lc_timer[1:] <= 0)[0] + 1:
            self.lc_timer[i] = self.rng.uniform(1.5, 3.0)
            if abs(self.d[i] - t.lane_d(self.lane[i])) > 0.3:
                continue  # 차선 변경 중
            if self.forced_brake is not None and self.forced_brake[0] == i:
                continue
            best, best_gain = None, self.level["lc_threshold"]
            for dl in (-1, 1):  # -1: 왼쪽(추월), +1: 오른쪽
                l = self.lane[i] + dl
                if l < 0 or l >= t.n_lanes or (self.is_heavy[i] and l == 0):
                    continue
                gain = self._mobil_gain(i, l, acc, v0)
                if gain is None:
                    continue
                gain += 0.3 if dl > 0 else -0.1   # 우측 차로 복귀 성향(1차로는 추월용)
                if gain > best_gain:
                    best, best_gain = l, gain
            if best is not None:
                self.lane[i] = best
                self.lane_changes += 1
                self.lc_timer[i] = self.rng.uniform(4.0, 7.0)   # 연속 차선 변경 방지

    def _mobil_gain(self, i, l, acc, v0):
        L = self.track.L
        p, b_safe = self.level["politeness"], self.level["b_safe"]
        td = float(self.track.lane_d(l))
        inlane = (np.abs(self.d - td) < 0.5 * self.width + 0.9) | (self.lane == l)
        inlane[0] = abs(self.d[0] - td) < 0.5 * self.width[0] + 0.9
        inlane[i] = False
        if not inlane.any():
            return float(idm_accel(self.v[i], v0[i], np.inf, self.v[i], self.a_max[i],
                                   self.b[i], self.T[i], self.s0[i]) - acc[i])
        ahead = np.mod(self.s - self.s[i], L)
        behind = np.mod(self.s[i] - self.s, L)
        half = 0.5 * (self.length + self.length[i])
        idx = np.where(inlane)[0]
        if (np.minimum(ahead[idx], behind[idx]) < half[idx] + 2.0).any():
            return None  # 옆에 차가 있음

        j = idx[np.argmin(ahead[idx])]
        gap_f = ahead[j] - half[j] if ahead[j] < 250 else np.inf
        a_new = idm_accel(self.v[i], v0[i], gap_f, self.v[j], self.a_max[i], self.b[i],
                          self.T[i], self.s0[i])
        if a_new < -b_safe:
            return None

        k = idx[np.argmin(behind[idx])]
        follower_gain = 0.0
        if behind[k] < 250:
            a_k_new = idm_accel(self.v[k], v0[k], behind[k] - half[k], self.v[i],
                                self.a_max[k], self.b[k], self.T[k], self.s0[k])
            if a_k_new < -b_safe:
                return None
            follower_gain = a_k_new - acc[k]
        return float(a_new - acc[i] + p * follower_gain)

    def _check_collisions(self):
        e, L = self.ego, self.track.L
        dsr = np.mod(self.s[1:] - self.s[0] + L / 2, L) - L / 2
        hit = (np.abs(dsr) < 0.5 * (self.length[1:] + e.length)) & \
              (np.abs(self.d[1:] - self.d[0]) < 0.5 * (self.width[1:] + e.width))
        new = hit & ~self._colliding
        if new.any():
            self.collisions += int(new.sum())
            e.vx *= 0.5
        self._colliding = hit

    def ai_overlaps(self):
        """AI 차량끼리 겹친 쌍 수 (테스트용, 0이어야 정상)."""
        L = self.track.L
        s, d = self.s[1:], self.d[1:]
        dsr = np.abs(np.mod(s[None, :] - s[:, None] + L / 2, L) - L / 2)
        ln, wd = self.length[1:], self.width[1:]
        hit = (dsr < 0.5 * (ln[:, None] + ln[None, :])) & \
              (np.abs(d[None, :] - d[:, None]) < 0.5 * (wd[:, None] + wd[None, :]))
        np.fill_diagonal(hit, False)
        return int(hit.sum() // 2)

    def _update_world_coords(self):
        x, y, psi = self.track.to_world(self.s, self.d)
        self.x, self.y = x, y
        self.yaw = psi + np.arctan2(self.vlat, np.maximum(self.v, 1.0))
        e = self.ego
        self.x[0], self.y[0], self.yaw[0] = e.x, e.y, e.yaw

    # ------------------------------------------------------------------
    def cycle_mode(self):
        self.ego_mode = MODES[(MODES.index(self.ego_mode) + 1) % len(MODES)]
        self.ego_target_lane = int(self.track.lane_of(self.ego.d)) if self.ego.road is None \
            else self.track.n_lanes - 1

    def change_lane(self, dl):
        self.ego_target_lane = int(np.clip(self.ego_target_lane + dl, 0, self.track.n_lanes - 1))

    def next_destination(self):
        """목적지 변경. 진출로 끝에 도착해 있으면 본선으로 복귀시켜 다시 출발."""
        e = self.ego
        if e.road is not None and self.nav.arrived:
            self._respawn_main(e.road.s0 + 500.0, self.track.n_lanes - 1, 70 / 3.6)
        self.nav.cycle_destination()

    def reset_ego(self):
        self._respawn_main(self.ego_s_main, 1 if self.track.n_lanes > 1 else 0, 0.0)

    def _respawn_main(self, s, lane, speed):
        """본선의 비어 있는 곳에 내 차를 다시 놓는다."""
        t, L = self.track, self.track.L
        td = float(t.lane_d(lane))
        for _ in range(100):
            ds = np.abs(np.mod(self.s[1:] - s + L / 2, L) - L / 2)
            if not ((ds < 40) & (np.abs(self.d[1:] - td) < 2.5)).any():
                break
            s += 20.0
        e = self.ego
        self._spawn_ego(e.kind, e.powertrain, s, lane, speed)
        self._sync_ego_arrays()
        self._colliding[:] = False
        self.nav.arrived = False
