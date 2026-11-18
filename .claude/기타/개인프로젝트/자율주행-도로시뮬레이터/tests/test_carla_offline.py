"""CARLA 버전 오프라인 테스트: 서버 없이 carla.Map(OpenDRIVE) 만으로 경로 탐색/직선 맵/제원 변환 확인.

carla 패키지가 없으면 건너뛴다 (pip install --no-index --find-links wheels -r requirements-carla.txt).
"""
import json
import os
import sys

import pytest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, ROOT)
carla = pytest.importorskip("carla")

from carla_sim.physics import curve_for  # noqa: E402
from carla_sim.route import RoutePlanner, guidance  # noqa: E402
from carla_sim.xodr import long_straight_xodr  # noqa: E402


def _ring_map():
    """도로 3개(직선-곡선-직선)가 고리로 이어진 2차로 맵."""
    def lane(i, pred, succ):
        link = "<link>" + (f'<predecessor id="{i}"/>' if pred else "") + (f'<successor id="{i}"/>' if succ else "") + "</link>"
        return f'<lane id="{i}" type="driving" level="false">{link}<width sOffset="0" a="3.5" b="0" c="0" d="0"/></lane>'
    def road(rid, pred, succ, geo):
        rl = (f'<predecessor elementType="road" elementId="{pred}" contactPoint="end"/>' if pred is not None else "") + \
             (f'<successor elementType="road" elementId="{succ}" contactPoint="start"/>' if succ is not None else "")
        lanes = lane(-1, pred is not None, succ is not None) + lane(-2, pred is not None, succ is not None)
        return f'<road name="r{rid}" length="200" id="{rid}" junction="-1"><link>{rl}</link><planView>{geo}</planView><lanes><laneSection s="0"><center><lane id="0" type="none" level="false"/></center><right>{lanes}</right></laneSection></lanes></road>'
    x = '<?xml version="1.0"?><OpenDRIVE><header revMajor="1" revMinor="4" name="t"/>' + \
        road(0,2,1,'<geometry s="0" x="0" y="0" hdg="0" length="200"><line/></geometry>') + \
        road(1,0,2,'<geometry s="0" x="200" y="0" hdg="0" length="200"><arc curvature="0.0078539816"/></geometry>') + \
        road(2,1,0,'<geometry s="0" x="327.32" y="127.32" hdg="1.5707963" length="200"><line/></geometry>') + '</OpenDRIVE>'
    return carla.Map("ring", x)


def test_long_straight_is_30km_and_unlimited():
    m = carla.Map("long", long_straight_xodr())
    end = m.get_waypoint_xodr(0, -3, 29990.0)
    assert end is not None and end.transform.location.x > 29900
    assert 'speed max="400"' in long_straight_xodr()


def test_route_on_straight_moves_to_target_lane():
    m = carla.Map("long", long_straight_xodr())
    r = RoutePlanner(m).trace(m.get_waypoint_xodr(0, -2, 30.0).transform.location,
                              m.get_waypoint_xodr(0, -3, 29700.0).transform.location)
    assert r[-1][0].lane_id == -3
    assert any(how == "change_right" for _, how in r)
    assert guidance(r, len(r) - 2, "End")["arrived"]


def test_route_across_roads_with_lane_change():
    m = _ring_map()
    g = m.get_waypoint_xodr(1, -2, 150.0).transform.location
    r = RoutePlanner(m).trace(m.get_waypoint_xodr(0, -1, 10.0).transform.location, g)
    assert r[-1][0].transform.location.distance(g) < 1.0
    assert {w.road_id for w, _ in r} == {0, 1}
    assert "Move to right lane" in guidance(r, 0, "G")["instruction"]


def test_powertrain_curves():
    specs = json.load(open(os.path.join(ROOT, "config", "vehicles.json"), encoding="utf-8"))
    car = specs["car"]
    ice, _, gears, _ = curve_for(car, "auto")
    assert max(t for _, t in ice) == pytest.approx(car["torque_nm"]) and gears == car["gears"]
    ev, mx, gears, _ = curve_for(car, "ev")
    assert mx == car["ev"]["max_motor_rpm"] and len(gears) == 1
    hy, *_ = curve_for(car, "hybrid")
    assert max(t for _, t in hy) > car["hybrid"]["engine_torque_nm"]
