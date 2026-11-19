"""절차적 사운드 (음원 파일 없이 numpy 로 생성).

- 엔진음: RPM 에 따른 음높이(구간별 미리 생성한 루프) + 가속 페달 부하에 따라 거친 저음 섞임
- 전기모터음: 모터 회전수에 비례하는 고음 윙~
- 타이어 스키드음: 미끄러짐 정도에 비례
- 풍절음: 속도 제곱에 비례
- 튜닝 파츠(ego.parts): 배기 = 엔진음 크고 거칠게, 흡기 = 흡입음, 터보 = 휘슬 + 페달 뗄 때 블로오프,
  전기 모터 = 모터음 크고 높게
오디오 장치가 없으면 조용히 꺼진다.
"""

import numpy as np

SR = 22050


def _loop(freq, dur, fn):
    cycles = max(1, round(freq * dur))
    n = int(SR * cycles / freq)
    t = np.arange(n) / SR
    return fn(t, freq)


def _engine_wave(load):
    def fn(t, f):
        w = np.zeros_like(t)
        for h, a in ((1, 1.0), (2, 0.6), (3, 0.35), (4, 0.25), (6, 0.12)):
            w += a * np.sin(2 * np.pi * f * h * t)
        if load:   # 부하: 저음 강조 + 거친 질감
            w += 0.8 * np.sign(np.sin(2 * np.pi * f * 0.5 * t)) * 0.4
            w += 0.25 * np.random.default_rng(1).standard_normal(len(t))
        return w
    return fn


def _noise(dur, smooth, seed):
    n = int(SR * dur)
    x = np.random.default_rng(seed).standard_normal(n)
    k = np.ones(smooth) / smooth
    x = np.convolve(np.concatenate([x[-smooth:], x]), k, mode="same")[smooth:]
    return x


class Sound:
    def __init__(self):
        self.ok = False
        self.enabled = True
        try:
            import pygame
            pygame.mixer.pre_init(SR, -16, 1, 512)
            pygame.mixer.init(SR, -16, 1, 512)
            if pygame.mixer.get_init() is None:
                return
            self.pg = pygame
            pygame.mixer.set_num_channels(8)
        except Exception:
            return
        try:
            self._build()
        except Exception:          # 소리 생성 실패는 시뮬레이터를 멈추지 않는다
            self.ok = False

    def _build(self):
        pygame = self.pg
        mk = self._mk
        self.eng_freqs = np.geomspace(18, 260, 26)
        self.eng = [(mk(_loop(f, 0.4, _engine_wave(False))), mk(_loop(f, 0.4, _engine_wave(True))))
                    for f in self.eng_freqs]
        self.ev_freqs = np.geomspace(150, 2200, 18)
        self.ev = [mk(_loop(f, 0.3, lambda t, f: np.sin(2 * np.pi * f * t) + 0.3 * np.sin(4 * np.pi * f * t)))
                   for f in self.ev_freqs]
        tire = _noise(1.0, 3, 2) + 0.5 * np.sin(2 * np.pi * 1300 * np.arange(SR) / SR)
        self.tire = mk(tire)
        self.wind = mk(_noise(2.0, 25, 3))
        self.turbo_freqs = np.geomspace(900, 7000, 18)
        self.turbo = [mk(_loop(f, 0.3, lambda t, f: np.sin(2 * np.pi * f * t) + 0.15 * np.sin(6 * np.pi * f * t)))
                      for f in self.turbo_freqs]
        n = int(SR * 0.45)
        self.bov = mk(_noise(0.45, 2, 5) * np.exp(-np.arange(n) / (SR * 0.12)))   # 블로오프 "치익"
        self.intake = mk(_noise(1.5, 6, 7))
        names = ("eng", "ev", "tire", "wind", "turbo", "bov", "intake")
        self.ch = {k: pygame.mixer.Channel(i) for i, k in enumerate(names)}
        self.cur = {"eng": None, "ev": None, "turbo": None}
        self._thr = 0.0
        self.ch["tire"].play(self.tire, loops=-1)
        self.ch["wind"].play(self.wind, loops=-1)
        self.ch["intake"].play(self.intake, loops=-1)
        for c in self.ch.values():
            c.set_volume(0.0)
        self.ok = True

    def _mk(self, w):
        w = w / (np.abs(w).max() + 1e-9) * 0.6
        a = (w * 32767).astype(np.int16)
        ch = self.pg.mixer.get_init()[2]          # pygame.init() 이 이미 스테레오로 열었을 수 있음
        if ch > 1:
            a = np.ascontiguousarray(np.repeat(a[:, None], ch, axis=1))
        return self.pg.sndarray.make_sound(a)

    def _set(self, name, snd, vol):
        if self.cur[name] is not snd:
            self.ch[name].play(snd, loops=-1)
            self.cur[name] = snd
        self.ch[name].set_volume(vol)

    def update(self, ego, throttle):
        if not self.ok:
            return
        if not self.enabled:
            for c in self.ch.values():
                c.set_volume(0.0)
            return
        v = abs(ego.vx)
        parts = getattr(ego, "parts", None) or {}
        ex, tb, ik, mo = (parts.get(k, 0) for k in ("exhaust", "turbo", "intake", "motor"))
        engine_running = ego.powertrain != "ev" and getattr(ego, "engine_on", True)
        if engine_running:
            cyl_f = max(ego.rpm, 1.0) / 60.0 * 2.0   # 4기통: 회전당 2번 폭발
            k = int(np.argmin(np.abs(self.eng_freqs - cyl_f)))
            loaded = throttle > (0.5 - 0.12 * ex)     # 배기 튜닝일수록 거친 소리가 일찍 섞임
            self._set("eng", self.eng[k][1 if loaded else 0], min(1.0, (0.18 + 0.3 * throttle) * (1 + 0.22 * ex)))
            spool = min(1.0, ego.rpm / getattr(ego, "redline", 6500)) * throttle
            if tb:
                k = int(np.argmin(np.abs(self.turbo_freqs - (900 + 5500 * spool))))
                self._set("turbo", self.turbo[k], 0.05 * tb * spool)
                if self._thr > 0.6 and throttle < 0.1 and ego.rpm > 2500:
                    self.ch["bov"].play(self.bov)
                    self.ch["bov"].set_volume(0.12 * tb)
            else:
                self.ch["turbo"].set_volume(0.0)
            self.ch["intake"].set_volume(0.06 * ik * throttle)
        else:
            for c in ("eng", "turbo", "intake"):
                self.ch[c].set_volume(0.0)
        self._thr = throttle
        if ego.powertrain in ("ev", "hybrid"):
            f = (150 + v * 45) * (1 + 0.12 * mo)
            k = int(np.argmin(np.abs(self.ev_freqs - f)))
            self._set("ev", self.ev[k], min(0.4, (0.02 + abs(ego.power_kw) / 800 + v / 400) * (1 + 0.35 * mo)))
        else:
            self.ch["ev"].set_volume(0.0)
        slip = 0.0
        if ego.sliding or ego.abs_active or ego.wheelspin:
            slip = 0.5
        if v > 3:
            slip = max(slip, min(1.0, abs(ego.vy) / 2.0))
        self.ch["tire"].set_volume(0.5 * slip)
        self.ch["wind"].set_volume(min(0.5, (v / 45.0) ** 2))
