"""OpenGL(ModernGL) 3D 렌더러: 조명(태양광), 그림자 맵, 안개, 텍스처 도로/잔디, 하늘·구름, 먼 산.

화면 밖(오프스크린) 프레임버퍼에 그린 뒤 RGB 배열로 읽어서 pygame 창에 붙인다.
그래서 창 모드, 스크린샷, env.py 의 화면 캡처(CUDA 비전 입력)가 모두 같은 경로를 쓴다.
시뮬레이션 코어(sim/)는 건드리지 않는다.
"""
import math

import numpy as np

SUN = np.array([-0.45, -0.35, -0.82])
SUN = SUN / np.linalg.norm(SUN)

# ----------------------------------------------------------------------------------------------
# 셰이더
# ----------------------------------------------------------------------------------------------
COMMON_FS = """
#version 330
uniform vec3 u_cam;
uniform vec3 u_sun;
uniform float u_fog;
uniform vec3 u_fogcol;
uniform sampler2D u_tex;
uniform sampler2DShadow u_shadow;
uniform int u_use_shadow;
in vec3 v_world; in vec3 v_norm; in vec2 v_uv; in vec3 v_col; in vec4 v_lpos; in float v_spec; in float v_emis;
out vec4 f_color;
float shadow() {
    if (u_use_shadow == 0) return 1.0;
    vec3 p = v_lpos.xyz / v_lpos.w * 0.5 + 0.5;
    if (p.x < 0.0 || p.x > 1.0 || p.y < 0.0 || p.y > 1.0 || p.z > 1.0) return 1.0;
    float s = 0.0;
    vec2 t = vec2(1.0 / 2048.0);
    for (int i = -1; i <= 1; i++) for (int j = -1; j <= 1; j++)
        s += texture(u_shadow, vec3(p.xy + vec2(i, j) * t, p.z - 0.0015));
    return s / 9.0;
}
void main() {
    vec3 n = normalize(v_norm);
    vec3 base = texture(u_tex, v_uv).rgb * v_col;
    float lam = max(dot(n, -u_sun), 0.0);
    float sh = shadow();
    vec3 sky = vec3(0.55, 0.65, 0.80);
    vec3 amb = mix(vec3(0.30, 0.28, 0.24), sky * 0.55, n.z * 0.5 + 0.5);
    vec3 col = base * (amb + vec3(1.0, 0.95, 0.85) * lam * sh * 0.95);
    vec3 v = normalize(u_cam - v_world);
    vec3 h = normalize(v - u_sun);
    col += v_spec * pow(max(dot(n, h), 0.0), 40.0) * sh;
    col += v_emis * v_col;
    float d = length(u_cam - v_world);
    float f = 1.0 - exp(-pow(d / u_fog, 2.0));
    col = mix(col, u_fogcol, clamp(f, 0.0, 1.0));
    f_color = vec4(pow(col, vec3(1.0 / 1.1)), 1.0);
}
"""

STATIC_VS = """
#version 330
uniform mat4 u_vp; uniform mat4 u_lvp; uniform vec3 u_offset;
in vec3 in_pos; in vec3 in_norm; in vec2 in_uv; in vec3 in_col;
out vec3 v_world; out vec3 v_norm; out vec2 v_uv; out vec3 v_col; out vec4 v_lpos; out float v_spec; out float v_emis;
void main() {
    vec3 p = in_pos + u_offset;
    v_world = p; v_norm = in_norm; v_uv = in_uv; v_col = in_col; v_spec = 0.0; v_emis = 0.0;
    v_lpos = u_lvp * vec4(p, 1.0);
    gl_Position = u_vp * vec4(p, 1.0);
}
"""

CAR_VS = """
#version 330
uniform mat4 u_vp; uniform mat4 u_lvp;
in vec3 in_pos; in vec3 in_norm; in float in_mat;
in mat4 in_model; in vec3 in_color; in float in_brake;
out vec3 v_world; out vec3 v_norm; out vec2 v_uv; out vec3 v_col; out vec4 v_lpos; out float v_spec; out float v_emis;
void main() {
    vec4 w = in_model * vec4(in_pos, 1.0);
    v_world = w.xyz;
    v_norm = normalize(mat3(in_model) * in_norm);
    v_uv = vec2(0.5);
    v_emis = 0.0; v_spec = 0.0;
    int m = int(in_mat + 0.5);
    if (m == 0) { v_col = in_color; v_spec = 0.35; }                       // 차체 도장
    else if (m == 1) { v_col = vec3(0.07, 0.09, 0.11); v_spec = 0.8; }     // 유리
    else if (m == 2) { v_col = vec3(0.04); }                               // 타이어
    else if (m == 3) { v_col = vec3(0.55, 0.03, 0.03); v_emis = in_brake * 2.0; }  // 후미등
    else if (m == 4) { v_col = vec3(0.9, 0.9, 0.85); v_emis = 0.3; }      // 전조등
    else { v_col = vec3(0.18); }                                          // 범퍼/하부
    v_lpos = u_lvp * w;
    gl_Position = u_vp * w;
}
"""

DEPTH_STATIC_VS = """
#version 330
uniform mat4 u_lvp;
in vec3 in_pos;
void main() { gl_Position = u_lvp * vec4(in_pos, 1.0); }
"""
DEPTH_CAR_VS = """
#version 330
uniform mat4 u_lvp;
in vec3 in_pos; in mat4 in_model;
void main() { gl_Position = u_lvp * in_model * vec4(in_pos, 1.0); }
"""
DEPTH_FS = """
#version 330
void main() {}
"""

SKY_VS = """
#version 330
in vec2 in_pos;
out vec2 v_ndc;
void main() { v_ndc = in_pos; gl_Position = vec4(in_pos, 0.9999, 1.0); }
"""
SKY_FS = """
#version 330
uniform mat4 u_inv;
uniform vec3 u_sun;
uniform vec3 u_cam;
uniform vec3 u_fogcol;
in vec2 v_ndc;
out vec4 f_color;
float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
float noise(vec2 p) {
    vec2 i = floor(p), f = fract(p);
    vec2 u = f * f * (3.0 - 2.0 * f);
    return mix(mix(hash(i), hash(i + vec2(1, 0)), u.x), mix(hash(i + vec2(0, 1)), hash(i + vec2(1, 1)), u.x), u.y);
}
float fbm(vec2 p) { float s = 0.0, a = 0.5; for (int i = 0; i < 5; i++) { s += a * noise(p); p *= 2.03; a *= 0.5; } return s; }
void main() {
    vec4 p = u_inv * vec4(v_ndc, 1.0, 1.0);
    vec3 d = normalize(p.xyz / p.w - u_cam);
    float t = clamp(d.z, 0.0, 1.0);
    vec3 col = mix(u_fogcol, vec3(0.25, 0.48, 0.82), pow(t, 0.55));
    float sd = max(dot(d, -u_sun), 0.0);
    col += vec3(1.0, 0.9, 0.7) * (pow(sd, 600.0) * 3.0 + pow(sd, 12.0) * 0.25);
    if (d.z > 0.01) {
        vec2 uv = d.xy / d.z * 0.0009 + u_cam.xy * 0.000002;
        float c = smoothstep(0.5, 0.78, fbm(uv * 3.0));
        col = mix(col, vec3(0.97, 0.97, 0.99), c * 0.85 * smoothstep(0.01, 0.15, d.z));
    }
    f_color = vec4(col, 1.0);
}
"""


# ----------------------------------------------------------------------------------------------
# 메시 생성
# ----------------------------------------------------------------------------------------------
class Mesh:
    """정적 메시: (N, 11) = pos3 norm3 uv2 col3."""

    def __init__(self):
        self.parts = []

    def add(self, P, N, UV, C):
        n = len(P)
        N = np.broadcast_to(np.asarray(N, np.float32), (n, 3))
        UV = np.broadcast_to(np.asarray(UV, np.float32), (n, 2))
        C = np.broadcast_to(np.asarray(C, np.float32), (n, 3))
        self.parts.append(np.hstack([np.asarray(P, np.float32), N, UV, C]))

    def strip(self, L, R, col, uvL=None, uvR=None, normal=(0, 0, 1)):
        """왼쪽/오른쪽 경계점 배열 (S, 3) 사이를 삼각형으로 채움."""
        S = len(L)
        if S < 2:
            return
        if uvL is None:
            uvL = np.zeros((S, 2))
            uvR = np.zeros((S, 2))
        i = np.arange(S - 1)
        P = np.stack([L[i], R[i], R[i + 1], L[i], R[i + 1], L[i + 1]], 1).reshape(-1, 3)
        UV = np.stack([uvL[i], uvR[i], uvR[i + 1], uvL[i], uvR[i + 1], uvL[i + 1]], 1).reshape(-1, 2)
        Nn = np.asarray(normal, float)
        if Nn.ndim == 2:   # 점마다 법선
            Nn = np.stack([Nn[i], Nn[i], Nn[i + 1], Nn[i], Nn[i + 1], Nn[i + 1]], 1).reshape(-1, 3)
        self.add(P, Nn, UV, col)

    def box(self, cx, cy, yaw, x0, x1, hw, z0, z1, col, top_col=None):
        c, s = math.cos(yaw), math.sin(yaw)

        def w(lx, ly, lz):
            return (cx + lx * c - ly * s, cy + lx * s + ly * c, lz)

        faces = [  # (꼭짓점 4개, 로컬 법선)
            ([(x1, -hw, z0), (x1, hw, z0), (x1, hw, z1), (x1, -hw, z1)], (1, 0, 0)),
            ([(x0, hw, z0), (x0, -hw, z0), (x0, -hw, z1), (x0, hw, z1)], (-1, 0, 0)),
            ([(x0, -hw, z0), (x1, -hw, z0), (x1, -hw, z1), (x0, -hw, z1)], (0, -1, 0)),
            ([(x1, hw, z0), (x0, hw, z0), (x0, hw, z1), (x1, hw, z1)], (0, 1, 0)),
            ([(x0, -hw, z1), (x1, -hw, z1), (x1, hw, z1), (x0, hw, z1)], (0, 0, 1)),
        ]
        for pts, (nx, ny, nz) in faces:
            q = [w(*p) for p in pts]
            n = (nx * c - ny * s, nx * s + ny * c, nz)
            self.add([q[0], q[1], q[2], q[0], q[2], q[3]], n, (0.5, 0.5),
                     top_col if (nz and top_col is not None) else col)

    def array(self):
        return np.concatenate(self.parts).astype(np.float32) if self.parts else np.zeros((0, 11), np.float32)


def _sphere(cx, cy, cz, r, rz, col, mesh, nlon=8, nlat=5):
    for a in range(nlat):
        t0, t1 = math.pi * a / nlat, math.pi * (a + 1) / nlat
        for b in range(nlon):
            p0, p1 = 2 * math.pi * b / nlon, 2 * math.pi * (b + 1) / nlon
            pts = []
            for t, p in ((t0, p0), (t1, p0), (t1, p1), (t0, p1)):
                n = (math.sin(t) * math.cos(p), math.sin(t) * math.sin(p), math.cos(t))
                pts.append(((cx + r * n[0], cy + r * n[1], cz + rz * n[2]), n))
            for i in (0, 1, 2, 0, 2, 3):
                mesh.add([pts[i][0]], pts[i][1], (0.5, 0.5), col)


def _prism(cx, cy, z0, z1, r, col, mesh, n=6):
    for k in range(n):
        a0, a1 = 2 * math.pi * k / n, 2 * math.pi * (k + 1) / n
        am = (a0 + a1) / 2
        p = [(cx + r * math.cos(a0), cy + r * math.sin(a0)), (cx + r * math.cos(a1), cy + r * math.sin(a1))]
        q = [(p[0][0], p[0][1], z0), (p[1][0], p[1][1], z0), (p[1][0], p[1][1], z1), (p[0][0], p[0][1], z1)]
        mesh.add([q[0], q[1], q[2], q[0], q[2], q[3]], (math.cos(am), math.sin(am), 0), (0.5, 0.5), col)


def _road_edges(road, ss, d0, d1, z):
    x0, y0, _ = road.to_world(ss, d0)
    x1, y1, _ = road.to_world(ss, d1)
    zz = np.full(len(ss), z)
    return np.stack([x0, y0, zz], 1), np.stack([x1, y1, zz], 1)


def _ground_strip(mesh, road, ss, d0, d1, z, col, tex_scale=None):
    L, R = _road_edges(road, ss, d0, d1, z)
    uvL = uvR = None
    if tex_scale:
        uvL = np.stack([np.full(len(ss), d0 / tex_scale), ss / tex_scale], 1)
        uvR = np.stack([np.full(len(ss), d1 / tex_scale), ss / tex_scale], 1)
    mesh.strip(L, R, col, uvL, uvR)


def _rail(mesh, road, ss, d, col=(0.75, 0.77, 0.8)):
    """가드레일: 빔(양면) + 기둥."""
    if len(ss) < 2:
        return
    B, T = _road_edges(road, ss, d, d, 0.45)
    T[:, 2] = 0.8
    _, _, psi = road.pose(ss)
    nrm = np.stack([-np.sin(psi), np.cos(psi), np.zeros_like(psi)], 1) * (1 if d < 0 else -1)
    mesh.strip(B, T, col, normal=nrm)
    mesh.strip(T, B, col, normal=-nrm)
    for k in range(0, len(ss), 2):
        x, y = B[k, 0], B[k, 1]
        mesh.box(x, y, float(psi[k]), -0.08, 0.08, 0.08, 0.0, 0.75, (0.5, 0.5, 0.52))


def _split_runs(mask):
    """True 구간들의 (시작, 끝) 인덱스."""
    runs, start = [], None
    for i, m in enumerate(mask):
        if m and start is None:
            start = i
        elif not m and start is not None:
            runs.append((start, i))
            start = None
    if start is not None:
        runs.append((start, len(mask)))
    return runs


def build_road_scene(track):
    """도로 코스의 정적 메시 묶음 {이름: (mesh array, texture name)}."""
    t = track
    hw, sh = t.half_width, t.shoulder
    ss = np.append(np.arange(0.0, t.L, 2.0), t.L)
    flat = {"grass": Mesh(), "asphalt": Mesh(), "lines": Mesh()}
    solid = Mesh()

    # 잔디 바닥 (코스 범위 + 여유)
    x0, x1 = t.x.min() - 3000, t.x.max() + 3000
    y0, y1 = t.y.min() - 3000, t.y.max() + 3000
    P = [(x0, y0, 0), (x1, y0, 0), (x1, y1, 0), (x0, y0, 0), (x1, y1, 0), (x0, y1, 0)]
    UV = [(x0 / 6, y0 / 6), (x1 / 6, y0 / 6), (x1 / 6, y1 / 6), (x0 / 6, y0 / 6), (x1 / 6, y1 / 6), (x0 / 6, y1 / 6)]
    flat["grass"].add(P, (0, 0, 1), UV, (1, 1, 1))
    for i in range(6):
        flat["grass"].parts[-1][i, 6:8] = UV[i]

    # 본선: 갓길(밝게) + 차도
    _ground_strip(flat["asphalt"], t, ss, hw + sh, hw, 0, (1.15, 1.15, 1.12), 4.0)
    _ground_strip(flat["asphalt"], t, ss, hw, -hw, 0, (1, 1, 1), 4.0)
    _ground_strip(flat["asphalt"], t, ss, -hw, -hw - sh, 0, (1.15, 1.15, 1.12), 4.0)
    _ground_strip(flat["lines"], t, ss, hw - 0.15, hw - 0.35, 0, (0.95, 0.75, 0.15))
    _ground_strip(flat["lines"], t, ss, -hw + 0.35, -hw + 0.15, 0, (0.95, 0.95, 0.95))
    starts = np.arange(0.0, t.L - 8, 20.0)
    for k in range(1, t.n_lanes):
        dk = hw - k * t.lane_width
        for st in starts:
            q = np.array([st, st + 8.0])
            _ground_strip(flat["lines"], t, q, dk + 0.08, dk - 0.08, 0, (0.95, 0.95, 0.95))

    # 가드레일 (진출로 분기 구간은 오른쪽 개방)
    _rail(solid, t, ss[::2], t.rail_d)
    gap = np.array([t.in_exit_zone(s, 60.0) for s in ss[::2]])
    for a, b in _split_runs(~gap):
        _rail(solid, t, ss[::2][a:b], -t.rail_d)

    # 진출로
    for r in t.ramps:
        rs = np.append(np.arange(0.0, r.L, 2.0), r.L)
        rh = r.half_width
        _ground_strip(flat["asphalt"], r, rs, rh + 1.0, -rh - 1.0, 0, (1.1, 1.1, 1.08), 4.0)
        _ground_strip(flat["asphalt"], r, rs, rh, -rh, 0, (1, 1, 1), 4.0)
        _ground_strip(flat["lines"], r, rs, -rh + 0.2, -rh, 0, (0.95, 0.95, 0.95))
        q = rs[rs > r.taper * 0.6]
        _ground_strip(flat["lines"], r, q, rh, rh - 0.2, 0, (0.95, 0.95, 0.95))
        _rail(solid, r, rs[::2], -r.rail_d)
        q = rs[rs > r.taper + 20][::2]
        _rail(solid, r, q, r.rail_d)

    # 나무
    rng = np.random.default_rng(5)
    for row in t.trees:
        _, _, tx, ty, crown, height = row
        g = rng.uniform(0.75, 1.1)
        _prism(tx, ty, 0, height * 0.6, 0.25, (0.35, 0.25, 0.16), solid)
        _sphere(tx, ty, height * 0.68, crown, crown * 1.15, (0.22 * g, 0.42 * g, 0.18 * g), solid)

    # 건물 (창문 포함)
    bcol = [(0.72, 0.68, 0.62), (0.6, 0.64, 0.7), (0.8, 0.76, 0.66), (0.5, 0.48, 0.46), (0.85, 0.85, 0.87)]
    for row in t.buildings:
        s, d, bx, by, L, W, H, ci = row
        _, _, psi = t.pose(s)
        psi = float(psi)
        col = bcol[int(ci)]
        solid.box(bx, by, psi, -L / 2, L / 2, W / 2, 0, H, col, top_col=tuple(c * 0.7 for c in col))
        c, sn = math.cos(psi), math.sin(psi)
        for side in (-1, 1):            # 긴 면 두 개에 창문 격자
            for zz in np.arange(2.0, H - 1.5, 3.2):
                for xx in np.arange(-L / 2 + 1.5, L / 2 - 1.5, 3.0):
                    ly = side * (W / 2 + 0.03)
                    pts = [(xx, ly, zz), (xx + 1.6, ly, zz), (xx + 1.6, ly, zz + 1.6), (xx, ly, zz + 1.6)]
                    q = [(bx + a * c - b * sn, by + a * sn + b * c, z) for a, b, z in pts]
                    if side < 0:
                        q = q[::-1]
                    solid.add([q[0], q[1], q[2], q[0], q[2], q[3]], (-side * sn, side * c, 0), (0.5, 0.5),
                              (0.15, 0.2, 0.28))

    # 출구 표지판 (기둥 + 판). 글자는 sign_texts 로 따로
    signs = []
    for s, name, dist in t.signs:
        d0 = -(t.rail_d + 1.0)
        _, _, psi = t.pose(s)
        for dd in (d0 + 1.8, d0 - 1.8):
            px, py, _ = t.to_world(s, dd)
            solid.box(float(px), float(py), float(psi), -0.12, 0.12, 0.12, 0, 7.2, (0.55, 0.55, 0.58))
        px, py, _ = t.to_world(s, d0)
        solid.box(float(px), float(py), float(psi), -0.18, -0.08, 2.6, 4.9, 7.3, (0.08, 0.42, 0.22))
        label = f"{name}  {dist if dist < 1000 else dist // 1000}{'m' if dist < 1000 else 'km'}"
        signs.append((float(px), float(py), float(psi), label))

    return {k: v.array() for k, v in flat.items()}, solid.array(), signs


def build_lot_scene(lot):
    flat = {"grass": Mesh(), "asphalt": Mesh(), "lines": Mesh()}
    solid = Mesh()
    x0, y0, x1, y1 = lot.rect
    G = 1500
    P = [(x0 - G, y0 - G, 0), (x1 + G, y0 - G, 0), (x1 + G, y1 + G, 0), (x0 - G, y0 - G, 0), (x1 + G, y1 + G, 0),
         (x0 - G, y1 + G, 0)]
    flat["grass"].add(P, (0, 0, 1), (0, 0), (1, 1, 1))
    for i, p in enumerate(P):
        flat["grass"].parts[-1][i, 6:8] = (p[0] / 6, p[1] / 6)
    P = [(x0, y0, 0), (x1, y0, 0), (x1, y1, 0), (x0, y0, 0), (x1, y1, 0), (x0, y1, 0)]
    flat["asphalt"].add(P, (0, 0, 1), (0, 0), (1.05, 1.05, 1.05))
    for i, p in enumerate(P):
        flat["asphalt"].parts[-1][i, 6:8] = (p[0] / 4, p[1] / 4)
    for (ax, ay), (bx, by), col in lot.lines:
        L = math.hypot(bx - ax, by - ay)
        if L < 1e-6:
            continue
        nx, ny = -(by - ay) / L * 0.07, (bx - ax) / L * 0.07
        q = [(ax + nx, ay + ny, 0), (ax - nx, ay - ny, 0), (bx - nx, by - ny, 0), (bx + nx, by + ny, 0)]
        c = tuple(v / 255 for v in col)
        flat["lines"].add([q[0], q[1], q[2], q[0], q[2], q[3]], (0, 0, 1), (0.5, 0.5), c)
    # 주차장 둘레 낮은 벽 + 나무 몇 그루
    for (ax, ay), (bx, by) in (((x0, y0), (x1, y0)), ((x1, y0), (x1, y1)), ((x1, y1), (x0, y1)), ((x0, y1), (x0, y0))):
        yaw = math.atan2(by - ay, bx - ax)
        solid.box((ax + bx) / 2, (ay + by) / 2, yaw, -math.hypot(bx - ax, by - ay) / 2,
                  math.hypot(bx - ax, by - ay) / 2, 0.15, 0, 0.6, (0.65, 0.65, 0.62))
    rng = np.random.default_rng(3)
    for k in range(40):
        a = rng.uniform(0, 2 * math.pi)
        r = rng.uniform(20, 80)
        tx = (x0 + x1) / 2 + math.cos(a) * ((x1 - x0) / 2 + r)
        ty = (y0 + y1) / 2 + math.sin(a) * ((y1 - y0) / 2 + r)
        h = rng.uniform(6, 11)
        _prism(tx, ty, 0, h * 0.6, 0.25, (0.35, 0.25, 0.16), solid)
        _sphere(tx, ty, h * 0.68, h * 0.35, h * 0.4, (0.22, 0.42, 0.18), solid)
    return {k: v.array() for k, v in flat.items()}, solid.array(), []


def build_mountains():
    """카메라를 따라다니는 먼 산 고리 (반경 5~6 km)."""
    m = Mesh()
    n = 180
    a = np.linspace(0, 2 * np.pi, n + 1)
    h = 260 + 180 * np.sin(3 * a + 1) + 120 * np.sin(7 * a + 2) + 60 * np.sin(13 * a)
    h2 = 160 + 90 * np.sin(5 * a + 0.3) + 70 * np.sin(11 * a + 1.2)
    for R, hh, col in ((6500, h, (0.45, 0.52, 0.6)), (5200, h2, (0.36, 0.46, 0.4))):
        B = np.stack([R * np.cos(a), R * np.sin(a), np.zeros_like(a)], 1)
        T = np.stack([R * np.cos(a), R * np.sin(a), hh], 1)
        nrm = -np.stack([np.cos(a), np.sin(a), np.zeros_like(a)], 1)
        m.strip(T, B, col, normal=nrm)
    return m.array()


def build_vehicle_meshes():
    """차종별 로컬 메시 (pos3 norm3 mat1), 기준 치수(m)로 만들고 인스턴스에서 실제 치수 비율로 스케일."""
    def box(out, x0, x1, y0, y1, z0, z1, mat, skip_bottom=True):
        faces = [
            ([(x1, y0, z0), (x1, y1, z0), (x1, y1, z1), (x1, y0, z1)], (1, 0, 0)),
            ([(x0, y1, z0), (x0, y0, z0), (x0, y0, z1), (x0, y1, z1)], (-1, 0, 0)),
            ([(x0, y0, z0), (x1, y0, z0), (x1, y0, z1), (x0, y0, z1)], (0, -1, 0)),
            ([(x1, y1, z0), (x0, y1, z0), (x0, y1, z1), (x1, y1, z1)], (0, 1, 0)),
            ([(x0, y0, z1), (x1, y0, z1), (x1, y1, z1), (x0, y1, z1)], (0, 0, 1)),
        ]
        for q, n in faces:
            for i in (0, 1, 2, 0, 2, 3):
                out.append((*q[i], *n, mat))

    def quad(out, q, n, mat):
        for i in (0, 1, 2, 0, 2, 3):
            out.append((*q[i], *n, mat))

    def wheel(out, cx, cy, r, w, n=10):
        for k in range(n):
            a0, a1 = 2 * math.pi * k / n, 2 * math.pi * (k + 1) / n
            p0 = (cx + r * math.cos(a0), r + r * math.sin(a0))
            p1 = (cx + r * math.cos(a1), r + r * math.sin(a1))
            am = (a0 + a1) / 2
            quad(out, [(p0[0], cy - w, p0[1]), (p1[0], cy - w, p1[1]), (p1[0], cy + w, p1[1]), (p0[0], cy + w, p0[1])],
                 (math.cos(am), 0, math.sin(am)), 2)
            for side in (-1, 1):   # 휠 옆면 (림)
                yy = cy + side * w
                tri = [(cx, yy, r), (p0[0], yy, p0[1]), (p1[0], yy, p1[1])]
                for p in tri:
                    out.append((*p, 0, side, 0, 5))

    def lights(out, L, W, zt, zb, inset=0.28):
        for side in (-1, 1):
            ya, yb = side * (W / 2 - 0.05), side * (W / 2 - inset)
            ys = sorted((ya, yb))
            quad(out, [(-L / 2 - 0.02, ys[1], zb), (-L / 2 - 0.02, ys[0], zb), (-L / 2 - 0.02, ys[0], zt),
                       (-L / 2 - 0.02, ys[1], zt)], (-1, 0, 0), 3)
            quad(out, [(L / 2 + 0.02, ys[0], zb), (L / 2 + 0.02, ys[1], zb), (L / 2 + 0.02, ys[1], zt),
                       (L / 2 + 0.02, ys[0], zt)], (1, 0, 0), 4)

    meshes = {}
    # 승용차 4.6 x 1.85 x 1.45
    L, W, H = 4.6, 1.85, 1.45
    o = []
    box(o, -L / 2, L / 2, -W / 2, W / 2, 0.28, 0.82, 0)
    box(o, -L / 2 - 0.05, -L / 2 + 0.2, -W / 2 + 0.05, W / 2 - 0.05, 0.25, 0.45, 5)
    box(o, L / 2 - 0.2, L / 2 + 0.05, -W / 2 + 0.05, W / 2 - 0.05, 0.25, 0.45, 5)
    # 캐빈: 아래는 유리, 지붕은 도장
    ct, cb = 0.18 * L, -0.30 * L
    quad(o, [(cb, -W / 2 + 0.08, 0.82), (ct, -W / 2 + 0.08, 0.82), (ct - 0.45, -W / 2 + 0.2, H - 0.02),
             (cb + 0.35, -W / 2 + 0.2, H - 0.02)], (0, -0.95, 0.3), 1)
    quad(o, [(ct, W / 2 - 0.08, 0.82), (cb, W / 2 - 0.08, 0.82), (cb + 0.35, W / 2 - 0.2, H - 0.02),
             (ct - 0.45, W / 2 - 0.2, H - 0.02)], (0, 0.95, 0.3), 1)
    quad(o, [(ct, -W / 2 + 0.08, 0.82), (ct, W / 2 - 0.08, 0.82), (ct - 0.45, W / 2 - 0.2, H - 0.02),
             (ct - 0.45, -W / 2 + 0.2, H - 0.02)], (0.6, 0, 0.8), 1)
    quad(o, [(cb, W / 2 - 0.08, 0.82), (cb, -W / 2 + 0.08, 0.82), (cb + 0.35, -W / 2 + 0.2, H - 0.02),
             (cb + 0.35, W / 2 - 0.2, H - 0.02)], (-0.6, 0, 0.8), 1)
    quad(o, [(cb + 0.35, -W / 2 + 0.2, H), (ct - 0.45, -W / 2 + 0.2, H), (ct - 0.45, W / 2 - 0.2, H),
             (cb + 0.35, W / 2 - 0.2, H)], (0, 0, 1), 0)
    lights(o, L, W, 0.78, 0.62)
    for wx in (L * 0.32, -L * 0.3):
        for wy in (W / 2 - 0.12, -W / 2 + 0.12):
            wheel(o, wx, wy, 0.33, 0.11)
    meshes["car"] = (np.array(o, np.float32), (L, W, H))

    # 트럭 12 x 2.5 x 3.6 (캐빈 + 화물칸)
    L, W, H = 12.0, 2.5, 3.6
    o = []
    box(o, -L / 2, L / 2 - 2.6, -W / 2, W / 2, 1.0, H, 0)                 # 화물칸
    box(o, -L / 2, L / 2, -W / 2 + 0.2, W / 2 - 0.2, 0.55, 1.0, 5)         # 차대
    box(o, L / 2 - 2.4, L / 2, -W / 2 + 0.02, W / 2 - 0.02, 0.55, 2.0, 0)  # 캐빈 아래
    box(o, L / 2 - 2.4, L / 2 - 0.25, -W / 2 + 0.02, W / 2 - 0.02, 2.0, 3.1, 0)
    quad(o, [(L / 2 - 0.25, -W / 2 + 0.05, 2.0), (L / 2 - 0.25, W / 2 - 0.05, 2.0), (L / 2 - 0.6, W / 2 - 0.05, 3.0),
             (L / 2 - 0.6, -W / 2 + 0.05, 3.0)], (0.9, 0, 0.4), 1)
    for side in (-1, 1):
        y = side * (W / 2 - 0.01)
        q = [(L / 2 - 1.9, y, 2.0), (L / 2 - 0.5, y, 2.0), (L / 2 - 0.5, y, 2.8), (L / 2 - 1.9, y, 2.8)]
        quad(o, q if side > 0 else q[::-1], (0, side, 0), 1)
    lights(o, L, W, 1.25, 1.0, 0.4)
    for wx in (L / 2 - 1.5, -L / 2 + 3.2, -L / 2 + 1.8):
        for wy in (W / 2 - 0.2, -W / 2 + 0.2):
            wheel(o, wx, wy, 0.52, 0.17)
    meshes["truck"] = (np.array(o, np.float32), (L, W, H))

    # 버스 12 x 2.5 x 3.3
    L, W, H = 12.0, 2.5, 3.3
    o = []
    box(o, -L / 2, L / 2, -W / 2, W / 2, 0.35, H, 0)
    for side in (-1, 1):   # 창문 띠
        y = side * (W / 2 + 0.015)
        q = [(-L / 2 + 0.6, y, H - 1.3), (L / 2 - 1.2, y, H - 1.3), (L / 2 - 1.2, y, H - 0.35),
             (-L / 2 + 0.6, y, H - 0.35)]
        quad(o, q if side > 0 else q[::-1], (0, side, 0), 1)
    quad(o, [(L / 2 + 0.015, -W / 2 + 0.1, 1.2), (L / 2 + 0.015, W / 2 - 0.1, 1.2), (L / 2 + 0.015, W / 2 - 0.1, H - 0.3),
             (L / 2 + 0.015, -W / 2 + 0.1, H - 0.3)], (1, 0, 0), 1)
    lights(o, L, W, 0.95, 0.65, 0.4)
    for wx in (L / 2 - 2.6, -L / 2 + 2.8):
        for wy in (W / 2 - 0.2, -W / 2 + 0.2):
            wheel(o, wx, wy, 0.5, 0.16)
    meshes["bus"] = (np.array(o, np.float32), (L, W, H))
    return meshes


# ----------------------------------------------------------------------------------------------
# 텍스처
# ----------------------------------------------------------------------------------------------
def _noise_tex(n, base, amp, seed, streak=False):
    rng = np.random.default_rng(seed)
    img = np.zeros((n, n))
    for octave, w in ((4, 0.5), (16, 0.3), (64, 0.15), (n, 0.08)):
        g = rng.random((octave, octave))
        reps = n // octave
        img += w * np.kron(g, np.ones((reps, reps)))
    img = (img - img.mean()) / (img.std() + 1e-6)
    if streak:   # 바퀴 자국 느낌의 세로 줄
        x = np.arange(n) / n
        img += 0.6 * (np.exp(-((x - 0.3) ** 2) / 0.002) + np.exp(-((x - 0.7) ** 2) / 0.002))[None, :]
    rgb = np.clip(np.array(base)[None, None, :] * (1 + amp * img[..., None]), 0, 255)
    return rgb.astype(np.uint8)


# ----------------------------------------------------------------------------------------------
# 렌더러
# ----------------------------------------------------------------------------------------------
def _perspective(fovy, aspect, near, far):
    f = 1 / math.tan(fovy / 2)
    m = np.zeros((4, 4))
    m[0, 0], m[1, 1] = f / aspect, f
    m[2, 2], m[2, 3] = (far + near) / (near - far), 2 * far * near / (near - far)
    m[3, 2] = -1
    return m


def _look(eye, fwd, up=(0, 0, 1)):
    f = np.asarray(fwd, float)
    f /= np.linalg.norm(f)
    s = np.cross(f, up)
    s /= np.linalg.norm(s)
    u = np.cross(s, f)
    m = np.eye(4)
    m[0, :3], m[1, :3], m[2, :3] = s, u, -f
    m[:3, 3] = -m[:3, :3] @ np.asarray(eye, float)
    return m


def _ortho(l, r, b, t, n, f):
    m = np.eye(4)
    m[0, 0], m[1, 1], m[2, 2] = 2 / (r - l), 2 / (t - b), -2 / (f - n)
    m[0, 3], m[1, 3], m[2, 3] = -(r + l) / (r - l), -(t + b) / (t - b), -(f + n) / (f - n)
    return m


def _mat(m):
    return np.ascontiguousarray(m.T, dtype=np.float32).tobytes()   # GLSL 은 열 우선


def create_context():
    import moderngl
    errors = []
    for kw in ({}, {"backend": "egl"}):
        try:
            return moderngl.create_standalone_context(require=330, **kw)
        except Exception as e:  # noqa: BLE001 - 백엔드마다 실패 이유가 다름
            errors.append(str(e))
    raise RuntimeError("OpenGL 3.3 컨텍스트를 만들 수 없음: " + " / ".join(errors))


class GLRenderer:
    def __init__(self, size, samples=4, shadows=True):
        import moderngl
        self.mgl = moderngl
        self.ctx = ctx = create_context()
        self.W, self.H = size
        self.shadows = shadows
        try:
            self.fbo_ms = ctx.framebuffer(ctx.renderbuffer(size, 4, samples=samples),
                                          ctx.depth_renderbuffer(size, samples=samples)) if samples else None
        except Exception:  # noqa: BLE001 - MSAA 미지원 드라이버
            self.fbo_ms = None
        self.fbo = ctx.framebuffer(ctx.renderbuffer(size, 4), ctx.depth_renderbuffer(size))
        self.prog = ctx.program(vertex_shader=STATIC_VS, fragment_shader=COMMON_FS)
        self.car_prog = ctx.program(vertex_shader=CAR_VS, fragment_shader=COMMON_FS)
        self.dprog = ctx.program(vertex_shader=DEPTH_STATIC_VS, fragment_shader=DEPTH_FS)
        self.dcar_prog = ctx.program(vertex_shader=DEPTH_CAR_VS, fragment_shader=DEPTH_FS)
        self.sky_prog = ctx.program(vertex_shader=SKY_VS, fragment_shader=SKY_FS)
        tri = np.array([-1, -1, 3, -1, -1, 3], np.float32)
        self.sky_vao = ctx.vertex_array(self.sky_prog, [(ctx.buffer(tri.tobytes()), "2f", "in_pos")])
        self.shadow_tex = ctx.depth_texture((2048, 2048))
        self.shadow_tex.compare_func = "<="
        self.shadow_tex.repeat_x = self.shadow_tex.repeat_y = False
        self.shadow_fbo = ctx.framebuffer(depth_attachment=self.shadow_tex)

        def tex(img, mip=True):
            t = ctx.texture((img.shape[1], img.shape[0]), 3, img.tobytes())
            if mip:
                t.build_mipmaps()
                t.filter = (moderngl.LINEAR_MIPMAP_LINEAR, moderngl.LINEAR)
                t.anisotropy = 8.0
            return t

        self.tex = {
            "grass": tex(_noise_tex(256, (92, 128, 62), 0.16, 1)),
            "asphalt": tex(_noise_tex(256, (92, 94, 97), 0.10, 2, streak=True)),
            "white": tex(np.full((4, 4, 3), 255, np.uint8)),
        }
        self.tex["lines"] = self.tex["white"]
        self.mountains = self._static_vao(build_mountains())
        self.vmesh = {}
        for kind, (arr, ref) in build_vehicle_meshes().items():
            vbo = ctx.buffer(arr.tobytes())
            self.vmesh[kind] = (vbo, len(arr), ref)
        self.inst = ctx.buffer(reserve=400 * 80, dynamic=True)
        self._scene_key = None
        self._target_key = None

    # ------------------------------------------------------------------
    def _static_vao(self, arr):
        if len(arr) == 0:
            return None
        vbo = self.ctx.buffer(arr.tobytes())
        vao = self.ctx.vertex_array(self.prog, [(vbo, "3f 3f 2f 3f", "in_pos", "in_norm", "in_uv", "in_col")])
        dvao = self.ctx.vertex_array(self.dprog, [(vbo, "3f 32x", "in_pos")])
        return vao, dvao

    def _sign_tex(self, label):
        import pygame
        if not pygame.font.get_init():
            pygame.font.init()
        f = pygame.font.SysFont("arial", 64, bold=True)
        img = f.render(label, True, (255, 255, 255), (20, 107, 56))
        surf = pygame.Surface((img.get_width() + 40, 110))
        surf.fill((20, 107, 56))
        pygame.draw.rect(surf, (240, 240, 240), surf.get_rect(), 4)
        surf.blit(img, (20, (110 - img.get_height()) // 2))
        arr = pygame.surfarray.array3d(surf).transpose(1, 0, 2)[::-1].copy()
        t = self.ctx.texture((arr.shape[1], arr.shape[0]), 3, arr.tobytes())
        t.build_mipmaps()
        t.filter = (self.mgl.LINEAR_MIPMAP_LINEAR, self.mgl.LINEAR)
        return t

    def _ensure_scene(self, world):
        key = id(getattr(world, "track", None) or world.lot)
        if key == self._scene_key:
            return
        self._scene_key = key
        flat, solid, signs = build_road_scene(world.track) if hasattr(world, "track") else build_lot_scene(world.lot)
        self.flat = [(self._static_vao(flat[k]), k) for k in ("grass", "asphalt", "lines")]
        self.solid = self._static_vao(solid)
        self.signs = []
        for px, py, psi, label in signs:
            c, s = math.cos(psi), math.sin(psi)
            w2 = 2.5
            pts = []
            for ly, z, u, v in ((w2, 5.0, 0, 0), (-w2, 5.0, 1, 0), (-w2, 7.2, 1, 1), (w2, 7.2, 0, 1)):
                lx = -0.2
                pts.append((px + lx * c - ly * s, py + lx * s + ly * c, z, u, v))
            m = Mesh()
            idx = (0, 1, 2, 0, 2, 3)
            m.add([pts[i][:3] for i in idx], (-c, -s, 0), [pts[i][3:] for i in idx], (1, 1, 1))
            self.signs.append((self._static_vao(m.array()), self._sign_tex(label)))
        self.target = None
        self._target_key = None

    def _ensure_target(self, world):
        if not hasattr(world, "lot"):
            return
        key = (world.lot.target, id(world.lot))
        if key == self._target_key:
            return
        self._target_key = key
        c = world.lot.spaces[world.lot.target].corners
        m = Mesh()
        for k in range(4):
            a, b = c[k], c[(k + 1) % 4]
            d = b - a
            n = np.array([-d[1], d[0]]) / (np.linalg.norm(d) + 1e-9) * 0.1
            q = [(*(a + n), 0), (*(a - n), 0), (*(b - n), 0), (*(b + n), 0)]
            m.add([q[0], q[1], q[2], q[0], q[2], q[3]], (0, 0, 1), (0.5, 0.5), (0.2, 0.9, 0.35))
        self.target = self._static_vao(m.array())

    # ------------------------------------------------------------------
    def _instances(self, world, fx, skip_ego):
        groups = {}
        for i in range(world.n):
            if i == 0 and skip_ego:
                continue
            kind = world.kind[i]
            _, _, (Lr, Wr, Hr) = self.vmesh[kind]
            yaw = float(world.yaw[i])
            c, s = math.cos(yaw), math.sin(yaw)
            R = np.array([[c, -s, 0], [s, c, 0], [0, 0, 1]])
            if i == 0 and fx is not None and fx.enabled:
                p, r = fx.body_pitch(), -fx.body_roll()
                Ry = np.array([[math.cos(p), 0, math.sin(p)], [0, 1, 0], [-math.sin(p), 0, math.cos(p)]])
                Rx = np.array([[1, 0, 0], [0, math.cos(r), -math.sin(r)], [0, math.sin(r), math.cos(r)]])
                R = R @ Ry @ Rx
            S = np.diag([world.length[i] / Lr, world.width[i] / Wr, world.height[i] / Hr])
            M = np.eye(4)
            M[:3, :3] = R @ S
            M[:3, 3] = (world.x[i], world.y[i], 0.0)
            col = np.array(world.color[i], float) / 255.0
            brake = 1.0 if world.accel[i] < -1.0 else 0.0
            groups.setdefault(kind, []).append(np.concatenate([M.T.ravel(), col, [brake]]))
        return {k: np.array(v, np.float32) for k, v in groups.items()}

    def _draw_cars(self, groups, prog, depth):
        for kind, data in groups.items():
            vbo, n, _ = self.vmesh[kind]
            if len(data) > 400:
                data = data[:400]
            self.inst.write(data.tobytes())
            if depth:
                vao = self.ctx.vertex_array(prog, [(vbo, "3f 12x 4x", "in_pos"),
                                                   (self.inst, "16f 16x/i", "in_model")])
            else:
                vao = self.ctx.vertex_array(prog, [(vbo, "3f 3f 1f", "in_pos", "in_norm", "in_mat"),
                                                   (self.inst, "16f 3f 1f/i", "in_model", "in_color", "in_brake")])
            vao.render(instances=len(data))
            vao.release()

    def render(self, world, mode, fx):
        """(H, W, 3) uint8 RGB 이미지."""
        from viz.render3d import make_camera
        ctx = self.ctx
        mgl = self.mgl
        self._ensure_scene(world)
        self._ensure_target(world)
        e = world.ego
        cam = make_camera(e, mode, self.W, self.H, fx)
        yaw = math.atan2(cam.s, cam.c)
        pitch = math.atan2(cam.sp, cam.cp)
        fovh = 2 * math.atan(self.W / 2 / cam.focal)
        fovy = 2 * math.atan(math.tan(fovh / 2) * self.H / self.W)
        eye = np.array([cam.x, cam.y, cam.h])
        fwd = np.array([math.cos(yaw) * math.cos(pitch), math.sin(yaw) * math.cos(pitch), -math.sin(pitch)])
        proj = _perspective(fovy, self.W / self.H, 0.3, 9000.0)
        view = _look(eye, fwd)
        vp = proj @ view
        # 그림자: 카메라 앞쪽 중심의 정사영 광원
        center = eye + np.array([fwd[0], fwd[1], 0]) / (np.hypot(fwd[0], fwd[1]) + 1e-9) * 60.0
        center[2] = 0
        lview = _look(center - SUN * 400, SUN, up=(0, 0, 1) if abs(SUN[2]) < 0.99 else (0, 1, 0))
        lvp = _ortho(-130, 130, -130, 130, 1, 900) @ lview
        groups = self._instances(world, fx, skip_ego=(mode == "cockpit"))
        fog_col = np.array([0.72, 0.8, 0.88])

        if self.shadows:
            self.shadow_fbo.use()
            self.shadow_fbo.clear(depth=1.0)
            ctx.enable(mgl.DEPTH_TEST)
            ctx.viewport = (0, 0, 2048, 2048)
            self.dprog["u_lvp"].write(_mat(lvp))
            self.dcar_prog["u_lvp"].write(_mat(lvp))
            if self.solid:
                self.solid[1].render()
            self._draw_cars(groups, self.dcar_prog, depth=True)

        fbo = self.fbo_ms or self.fbo
        fbo.use()
        ctx.viewport = (0, 0, self.W, self.H)
        fbo.clear(*fog_col, 1.0, depth=1.0)
        # 하늘
        ctx.disable(mgl.DEPTH_TEST)
        sp = self.sky_prog
        sp["u_inv"].write(_mat(np.linalg.inv(vp)))
        sp["u_sun"].value = tuple(SUN)
        sp["u_cam"].value = tuple(eye)
        sp["u_fogcol"].value = tuple(fog_col)
        self.sky_vao.render()

        for prog in (self.prog, self.car_prog):
            prog["u_vp"].write(_mat(vp))
            prog["u_lvp"].write(_mat(lvp))
            prog["u_cam"].value = tuple(eye)
            prog["u_sun"].value = tuple(SUN)
            prog["u_fogcol"].value = tuple(fog_col)
            prog["u_tex"].value = 0
            prog["u_shadow"].value = 1
            prog["u_use_shadow"].value = 1 if self.shadows else 0
        self.shadow_tex.use(location=1)
        # 먼 산 (카메라 따라감, 안개 약하게)
        self.prog["u_fog"].value = 30000.0
        self.prog["u_offset"].value = (cam.x, cam.y, -5.0)
        self.prog["u_use_shadow"].value = 0
        self.tex["white"].use(location=0)
        self.mountains[0].render()
        self.prog["u_offset"].value = (0.0, 0.0, 0.0)
        self.prog["u_use_shadow"].value = 1 if self.shadows else 0
        self.prog["u_fog"].value = float(world.fog_dist) * 1.3
        self.car_prog["u_fog"].value = float(world.fog_dist) * 1.3
        # 평평한 층: 깊이 테스트 없이 순서대로 (잔디 -> 아스팔트 -> 차선)
        for vaos, name in self.flat:
            if vaos:
                self.tex[name].use(location=0)
                vaos[0].render()
        if self.target:
            self.tex["white"].use(location=0)
            self.target[0].render()
        # 입체 물체
        ctx.enable(mgl.DEPTH_TEST)
        self.tex["white"].use(location=0)
        if self.solid:
            self.solid[0].render()
        for vaos, t in self.signs:
            t.use(location=0)
            vaos[0].render()
        self.tex["white"].use(location=0)
        self._draw_cars(groups, self.car_prog, depth=False)

        if self.fbo_ms:
            ctx.copy_framebuffer(self.fbo, self.fbo_ms)
        data = self.fbo.read(components=3, alignment=1)
        return np.frombuffer(data, np.uint8).reshape(self.H, self.W, 3)[::-1]
