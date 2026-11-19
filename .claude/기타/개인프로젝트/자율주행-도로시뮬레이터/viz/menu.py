"""시뮬레이터 안 설정 메뉴 (Tab / Esc 로 열기).

↑↓ 항목 선택, ←→ 값 변경(Shift 를 누르면 크게), Enter 실행. 메뉴가 열려 있는 동안은 일시정지.
화면 글자는 sim/i18n.py 로 한국어(기본)/영어 전환 ('언어' 항목).
코스/난이도/트래픽/차로 변경은 월드를 새로 만들고, 차량 제원 변경은 현재 위치에서 차만 바꾼다.
"Save" 는 차량 제원을 config/vehicles.json 에 저장한다.
"""
import copy
import json

import pygame

from sim import i18n
from sim.i18n import tr
from sim.vehicle import CONFIG, POWERTRAINS, load_specs

LEVELS = ("easy", "normal", "hard", "long", "parking")
LEVEL_NAME = {"easy": "Easy (beginner)", "normal": "Normal", "hard": "Hard (expert)",
              "long": "Long straight 30 km", "parking": "Parking lot"}
KINDS = ("car", "bus", "truck")
PT_NAME = {"auto": "Engine - automatic", "manual": "Engine - manual", "ev": "Electric", "hybrid": "Hybrid"}

# (라벨, 제원 경로, 최소, 최대, 표시 형식) - 값은 ←→ 한 번에 5% (Shift 20%)씩 바뀜
SPEC_ITEMS = [
    ("Engine power [hp]", ("power_hp",), 30, 2000, "{:.0f}"),
    ("Engine torque [Nm]", ("torque_nm",), 50, 6000, "{:.0f}"),
    ("Redline [rpm]", ("redline_rpm",), 2000, 12000, "{:.0f}"),
    ("Final drive (lower=faster)", ("final_drive",), 1.5, 8.0, "{:.2f}"),
    ("Brake force [N]", ("brake_force_n",), 2000, 300000, "{:.0f}"),
    ("Mass [kg]", ("mass",), 500, 40000, "{:.0f}"),
    ("Tire grip mu", ("tire_mu",), 0.3, 1.6, "{:.2f}"),
    ("Max steer [deg]", ("max_steer_deg",), 10, 60, "{:.0f}"),
    ("Speed limiter [km/h] (0=off)", ("speed_limiter_kmh",), 0, 400, "{:.0f}"),
    ("EV motor power [kW]", ("ev", "motor_power_kw"), 20, 2000, "{:.0f}"),
    ("EV motor torque [Nm]", ("ev", "motor_torque_nm"), 50, 8000, "{:.0f}"),
    ("EV reduction (lower=faster)", ("ev", "reduction"), 2.0, 15.0, "{:.2f}"),
    ("EV battery [kWh]", ("ev", "battery_kwh"), 5, 1000, "{:.0f}"),
    ("Hybrid engine power [hp]", ("hybrid", "engine_hp"), 30, 1500, "{:.0f}"),
    ("Hybrid motor power [hp]", ("hybrid", "motor_hp"), 10, 1000, "{:.0f}"),
]


def _get(spec, path):
    for k in path:
        spec = spec[k]
    return spec


def _set(spec, path, val):
    for k in path[:-1]:
        spec = spec[k]
    spec[path[-1]] = val


class SettingsMenu:
    def __init__(self, app):
        self.app = app
        self.open = False
        self.sel = 0
        self.msg = ""
        self.f_title = i18n.font(28, bold=True)       # 한글 글꼴
        self.f = i18n.font(18)                        # 작으면 일부 한글 받침이 뭉개짐

    # ------------------------------------------------------------------
    def items(self):
        a = self.app
        e = a.world.ego
        cars = tr("auto") if a.cfg["cars"] is None else str(a.cfg["cars"])
        onoff = lambda b: tr("on") if b else tr("off")  # noqa: E731
        rows = [
            ("resume", ">>> START / RESUME DRIVING  (Enter)  <<<", ""),
            ("header", "SCENARIO", None),
            ("level", "Course / difficulty", tr(LEVEL_NAME[a.cfg["level"]])),
            ("cars", "Traffic cars", cars),
            ("lanes", "Lanes", str(a.cfg["lanes"])),
            ("seed", "Random seed", str(a.cfg["seed"])),
            ("header", "VEHICLE", None),
            ("kind", "Vehicle type", tr(e.spec["name"])),
            ("powertrain", "Powertrain", tr(PT_NAME[e.powertrain])),
            ("header", "DISPLAY / SOUND", None),
            ("lang", "Language", i18n.LANG_NAME[i18n.LANG]),
            ("view", "Camera view", tr(a.view)),
            ("fx", "Camera effects", onoff(a.fx.enabled)),
            ("sound", "Sound", onoff(a.sound and a.sound.enabled)),
            ("help", "Key help overlay", onoff(a.hud.show_help)),
            ("header", "", None),
            ("save", "> Save vehicle settings to config/vehicles.json", ""),
            ("defaults", "> Reload settings from file", ""),
            ("quit", "> Quit simulator", ""),
            ("column", "", None),                    # 여기부터 오른쪽 칸
            ("header", "VEHICLE SPECS", None),
        ]
        for k, (label, path, lo, hi, fmt) in enumerate(SPEC_ITEMS):
            rows.append((("spec", k), label, fmt.format(_get(a.specs[e.kind], path))))
        return [(k, tr(label), val) for k, label, val in rows]   # 라벨은 화면 언어로

    def _selectable(self):
        return [i for i, r in enumerate(self.items()) if r[0] not in ("header", "column")]

    # ------------------------------------------------------------------
    def handle(self, ev):
        """KEYDOWN 이벤트 처리. 'quit' 이면 종료 요청."""
        if ev.type != pygame.KEYDOWN:
            return None
        sel = self._selectable()
        if self.sel not in sel:
            self.sel = sel[0]
        k = ev.key
        if k in (pygame.K_TAB, pygame.K_ESCAPE):
            self.open = False
        elif k == pygame.K_UP:
            self.sel = sel[(sel.index(self.sel) - 1) % len(sel)]
        elif k == pygame.K_DOWN:
            self.sel = sel[(sel.index(self.sel) + 1) % len(sel)]
        elif k in (pygame.K_LEFT, pygame.K_RIGHT, pygame.K_RETURN, pygame.K_KP_ENTER):
            step = -1 if k == pygame.K_LEFT else 1
            big = bool(ev.mod & pygame.KMOD_SHIFT)
            return self._change(self.items()[self.sel][0], step, big, k in (pygame.K_RETURN, pygame.K_KP_ENTER))
        return None

    def _change(self, key, step, big, enter):
        a = self.app
        e = a.world.ego
        self.msg = ""
        if key == "level":
            a.cfg["level"] = LEVELS[(LEVELS.index(a.cfg["level"]) + step) % len(LEVELS)]
            a.rebuild()
        elif key == "cars":
            cur = a.cfg["cars"]
            cur = (a.world.n - 1 if hasattr(a.world, "track") else 60) if cur is None else cur
            a.cfg["cars"] = max(0, min(400, cur + step * (50 if big else 5)))
            a.rebuild()
        elif key == "lanes":
            a.cfg["lanes"] = max(2, min(5, a.cfg["lanes"] + step))
            a.rebuild()
        elif key == "seed":
            a.cfg["seed"] = max(0, a.cfg["seed"] + step)
            a.rebuild()
        elif key == "kind":
            a.world.set_vehicle(kind=KINDS[(KINDS.index(e.kind) + step) % len(KINDS)])
        elif key == "powertrain":
            a.world.set_vehicle(powertrain=POWERTRAINS[(POWERTRAINS.index(e.powertrain) + step) % len(POWERTRAINS)])
        elif isinstance(key, tuple) and key[0] == "spec":
            label, path, lo, hi, fmt = SPEC_ITEMS[key[1]]
            spec = a.specs[e.kind]
            v = _get(spec, path)
            nv = v * (1 + step * (0.2 if big else 0.05))
            if path == ("speed_limiter_kmh",):
                nv = v + step * (50 if big else 10)
            elif isinstance(v, int) or fmt == "{:.0f}":
                nv = round(nv) if abs(nv - v) >= 1 else v + step
            else:
                nv = round(nv, 2)
            nv = max(lo, min(hi, nv))
            if path == ("redline_rpm",):     # 변속 시점도 같은 비율로
                for k in ("shift_up_rpm", "shift_down_rpm"):
                    spec[k] = round(spec[k] * nv / v)
            _set(spec, path, nv)
            a.world.set_vehicle()          # 현재 위치에서 새 제원으로
        elif key == "lang":
            i18n.set_lang(i18n.LANGS[(i18n.LANGS.index(i18n.LANG) + step) % len(i18n.LANGS)])
        elif key == "view":
            views = ("cockpit", "chase", "top")
            a.view = views[(views.index(a.view) + step) % len(views)]
        elif key == "fx":
            a.fx.enabled = not a.fx.enabled
        elif key == "sound" and a.sound:
            a.sound.enabled = not a.sound.enabled
        elif key == "help":
            a.hud.show_help = not a.hud.show_help
        elif key == "save" and enter:
            self.save()
        elif key == "defaults" and enter:
            a.specs.clear()
            a.specs.update(load_specs())
            a.world.specs = a.specs
            a.world.set_vehicle()
            self.msg = tr("Reloaded from config/vehicles.json")
        elif key == "resume" and enter:
            self.open = False
            if getattr(a, "garage", None) is not None and a.garage.open:   # CARLA: 정비소에서 열었으면 바로 출발
                a.garage.leave()
        elif key == "quit" and enter:
            return "quit"
        return None

    def save(self):
        with open(CONFIG, encoding="utf-8") as f:
            data = json.load(f)
        for kind, spec in self.app.specs.items():
            data[kind] = copy.deepcopy(spec)
        with open(CONFIG, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
        self.msg = tr("Saved to config/vehicles.json")

    # ------------------------------------------------------------------
    def draw(self, surf):
        W, H = surf.get_size()
        shade = pygame.Surface((W, H), pygame.SRCALPHA)
        shade.fill((5, 8, 12, 245))
        surf.blit(shade, (0, 0))
        rows = self.items()
        sel = self._selectable()
        if self.sel not in sel:
            self.sel = sel[0]
        cw = min(600, (W - 60) // 2)                 # 두 칸: 왼쪽 코스·차량·화면, 오른쪽 차량 제원
        x0, y0, lh, vx = W // 2 - cw - 10, 50, 24, int(cw * 0.56)
        surf.blit(self.f_title.render(tr("SETTINGS"), True, (255, 210, 60)), (x0, y0 - 40))
        hint = tr("Up/Down select   Left/Right change (Shift = big step)   Enter = run   Tab/Esc = close")
        surf.blit(self.f.render(hint, True, (160, 170, 180)), (x0, H - 36))
        hx, y = x0, y0 + 10
        for i, (key, label, val) in enumerate(rows):
            if key == "column":
                x0, y = W // 2 + 10, y0 + 10
                continue
            if key == "header":
                y += 6
                surf.blit(self.f.render(label, True, (120, 200, 255)), (x0, y))
            else:
                on = i == self.sel
                if on:
                    pygame.draw.rect(surf, (40, 70, 110), (x0 - 6, y - 1, cw, lh))
                col = (255, 255, 255) if on else (205, 210, 215)
                surf.blit(self.f.render(label, True, col), (x0 + 12, y))
                if val:
                    txt = f"<  {val}  >" if on else val
                    surf.blit(self.f.render(txt, True, (255, 230, 120) if on else col), (x0 + vx, y))
            y += lh
        if self.msg:
            surf.blit(self.f.render(self.msg, True, (120, 240, 140)), (hx, H - 64))
