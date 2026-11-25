"""목적지까지 경로 탐색 + 안내 (CARLA 맵의 OpenDRIVE 토폴로지 사용, 추가 패키지 없음).

CARLA 의 agents/GlobalRoutePlanner 대신 직접 구현: networkx/shapely 없이 폐쇄망에서 돌게 하려고.
  - 노드 = 토폴로지의 차로 구간(road, section, lane), 간선 = 이어지는 구간 + 옆 차로(차선 변경)
  - 다익스트라로 최단 경로, 결과는 2 m 간격 waypoint 목록 + 각 지점의 안내(직진/좌회전/우회전/차로 변경)
"""
import heapq
import math

from sim.i18n import tr

STEP = 2.0
LANE_CHANGE_COST = 25.0


def _yaw_diff(a, b):
    return (b - a + 180.0) % 360.0 - 180.0


def _key(wp):
    return wp.road_id, wp.section_id, wp.lane_id


def _driving(wp):
    return wp is not None and str(wp.lane_type) == "Driving"


class RoutePlanner:
    def __init__(self, cmap):
        self.map = cmap
        self.segs = []          # 구간별 waypoint 목록
        self.index = {}         # (road, section, lane) -> 구간 번호
        for a, b in cmap.get_topology():
            pts = [a] + a.next_until_lane_end(STEP)
            if pts[-1].transform.location.distance(b.transform.location) > 0.5:
                pts.append(b)
            self.index.setdefault(_key(a), len(self.segs))
            self.segs.append(pts)
        starts = {}
        for i, pts in enumerate(self.segs):
            loc = pts[0].transform.location
            starts.setdefault((round(loc.x), round(loc.y)), []).append(i)
        self.next = []
        for pts in self.segs:
            loc = pts[-1].transform.location
            cand = set()
            for dx in (-1, 0, 1):
                for dy in (-1, 0, 1):
                    for j in starts.get((round(loc.x) + dx, round(loc.y) + dy), []):
                        if self.segs[j][0].transform.location.distance(loc) < 1.0:
                            cand.add(j)
            self.next.append(sorted(cand))

    # ------------------------------------------------------------------
    def _neighbours(self, i):
        yield from ((j, len(self.segs[j]) * STEP, "follow") for j in self.next[i])
        wp = self.segs[i][0]
        for side, lane in (("left", wp.get_left_lane()), ("right", wp.get_right_lane())):
            if _driving(lane) and lane.lane_id * wp.lane_id > 0 and _key(lane) in self.index:
                yield self.index[_key(lane)], LANE_CHANGE_COST, side

    def trace(self, start_loc, goal_loc):
        """[(waypoint, 안내)] 목록. 안내: 'straight' / 'left' / 'right' / 'change_left' / 'change_right'."""
        s = self.map.get_waypoint(start_loc)
        g = self.map.get_waypoint(goal_loc)
        if not self.segs or _key(s) not in self.index or _key(g) not in self.index:
            return self._simple(s, g)
        si, gi = self.index[_key(s)], self.index[_key(g)]
        dist, prev = {si: 0.0}, {}
        heap = [(0.0, si)]
        while heap:
            d, i = heapq.heappop(heap)
            if i == gi:
                break
            if d > dist.get(i, math.inf):
                continue
            for j, c, how in self._neighbours(i):
                if d + c < dist.get(j, math.inf):
                    dist[j], prev[j] = d + c, (i, how)
                    heapq.heappush(heap, (d + c, j))
        if gi not in dist:
            return self._simple(s, g)
        chain = [(gi, "follow")]
        while chain[-1][0] != si:
            chain.append(prev[chain[-1][0]])
        chain.reverse()      # [(seg, 다음 구간으로 넘어가는 방법)], 마지막 항목의 방법은 무시
        out = []
        frac = self._fraction(si, s)
        for n, (i, how) in enumerate(chain):
            how_next = how if n + 1 < len(chain) else None
            pts = self.segs[i]
            a = int(frac * (len(pts) - 1))
            frac = 0.0
            if how_next in ("left", "right"):          # 이 구간 일부만 달리고 옆 차로로
                b = min(len(pts), a + 15)
                frac = b / max(len(pts) - 1, 1)
            else:
                b = len(pts)
            if i == gi:
                b = min(b, self._index_of(gi, g) + 1)
            seg_pts = pts[a:b]
            label = self._turn(pts) if pts[0].is_junction else "straight"
            out += [(w, label) for w in seg_pts]
            if how_next in ("left", "right") and out:
                out[-1] = (out[-1][0], "change_" + how_next)
        return out

    def _fraction(self, i, wp):
        return self._index_of(i, wp) / max(len(self.segs[i]) - 1, 1)

    def _index_of(self, i, wp):
        loc = wp.transform.location
        return min(range(len(self.segs[i])), key=lambda k: self.segs[i][k].transform.location.distance(loc))

    @staticmethod
    def _turn(pts):
        d = _yaw_diff(pts[0].transform.rotation.yaw, pts[-1].transform.rotation.yaw)
        return "right" if d > 35 else ("left" if d < -35 else "straight")   # CARLA yaw 는 시계방향 +

    def _simple(self, s, g):
        """토폴로지가 없는 맵(긴 직선 등): 현재 차로로 목적지 앞까지 가서 목적지 차로로 옮긴다."""
        out, wp = [], s
        goal = g.transform.location
        for _ in range(int(s.transform.location.distance(goal) / STEP) + 2):
            out.append((wp, "straight"))
            if wp.transform.location.distance(goal) < STEP * 1.5:
                break
            nxt = wp.next(STEP)
            if not nxt:
                break
            wp = nxt[0]
        if out and wp.lane_id != g.lane_id:
            side = "left" if abs(g.lane_id) < abs(wp.lane_id) else "right"
            k = max(0, len(out) - 60)
            out[k] = (out[k][0], "change_" + side)
            out.append((g, "straight"))
        return out


TEXT = {"left": "Turn left", "right": "Turn right", "change_left": "Move to left lane",
        "change_right": "Move to right lane"}


def guidance(route, idx, dest_name):
    """route[idx] 에 있는 내 차 기준 안내 dict (기존 sim/nav.py guidance 와 같은 키)."""
    if not route:
        return dict(active=False, destination=None, distance=None, instruction=tr("No destination (press N)"),
                    arrow=None, arrived=False, required_lane=None, distance_to_exit=None)
    remain = (len(route) - 1 - idx) * STEP
    if remain < 15:
        return dict(active=True, destination=dest_name, distance=0.0, instruction=tr("Arrived: {dest}", dest=dest_name),
                    arrow="arrive", arrived=True, required_lane=None, distance_to_exit=0.0)
    for k in range(idx, len(route)):
        what = route[k][1]
        if what != "straight":
            d = (k - idx) * STEP
            return dict(active=True, destination=dest_name, distance=remain, distance_to_exit=d,
                        instruction=tr("{what} in {d:.0f} m", what=tr(TEXT[what]), d=d), arrow=what, arrived=False,
                        required_lane=route[min(k + 1, len(route) - 1)][0].lane_id)
    return dict(active=True, destination=dest_name, distance=remain, distance_to_exit=None,
                instruction=tr("Go straight  {km:.1f} km to {dest}", km=remain / 1000, dest=dest_name), arrow="straight",
                arrived=False, required_lane=None)
