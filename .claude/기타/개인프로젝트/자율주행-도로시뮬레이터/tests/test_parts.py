"""정비소 파츠 테스트: python -m pytest tests -q"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from sim import parts as P  # noqa: E402
from sim.vehicle import load_specs  # noqa: E402


def test_stock_unchanged_and_parts_raise_power():
    specs = load_specs()
    base = specs["car"]
    assert P.apply_parts(base, {}, "auto") == base
    tuned = P.apply_parts(base, {"turbo": 2, "ecu": 3}, "auto")
    assert tuned["power_hp"] > base["power_hp"] and tuned["torque_nm"] > base["torque_nm"]
    assert tuned["redline_rpm"] == base["redline_rpm"] + 500
    assert base["power_hp"] == load_specs()["car"]["power_hp"]          # 원본은 그대로


def test_parts_only_for_matching_powertrain():
    base = load_specs()["car"]
    assert P.apply_parts(base, {"motor": 3}, "auto") == base             # 엔진차에 모터 파츠는 효과 없음
    ev = P.apply_parts(base, {"motor": 3, "battery": 2, "turbo": 3}, "ev")
    assert ev["ev"]["motor_power_kw"] > base["ev"]["motor_power_kw"]
    assert ev["mass"] > base["mass"] and ev["power_hp"] == base["power_hp"]
    assert {p[0] for p in P.applicable("hybrid")} == {p[0] for p in P.PARTS}


def test_performance_improves_with_race_parts():
    base = load_specs()["car"]
    full = {p[0]: 3 for p in P.PARTS}
    a = P.performance(base, "car", "auto")
    b = P.performance(P.apply_parts(base, full, "auto"), "car", "auto")
    assert 150 < a["top_kmh"] < b["top_kmh"]
    assert a["t100"] is not None and b["t100"] <= a["t100"] and b["t200"] < a["t200"]


def test_save_and_load(tmp_path):
    path = tmp_path / "garage.json"
    P.save_parts({"car": {"turbo": 2, "nope": 1}}, path)
    assert P.load_parts(path) == {"car": {"turbo": 2}}
    assert P.load_parts(tmp_path / "missing.json") == {}
