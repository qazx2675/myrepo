"""3D 뷰 렌더러 선택: gl (ModernGL, 조명/그림자) 또는 classic (pygame 2D 폴리곤).

gl 을 만들 수 없으면(ModernGL 미설치, OpenGL 미지원) classic 으로 자동 대체한다.
"""


class ClassicScene:
    def draw(self, surf, world, mode, fx):
        from viz.render3d import draw_scene
        draw_scene(surf, world, mode, fx)


class GLScene:
    def __init__(self, size):
        from viz.gl3d import GLRenderer
        self.r = GLRenderer(size)

    def draw(self, surf, world, mode, fx):
        import pygame
        img = self.r.render(world, mode, fx)            # (H, W, 3)
        pygame.surfarray.blit_array(surf, img.swapaxes(0, 1))


def make_scene_renderer(kind, size):
    if kind == "gl":
        try:
            return GLScene(size)
        except Exception as ex:  # noqa: BLE001 - OpenGL 이 안 되는 PC 는 classic 으로
            print("OpenGL renderer unavailable, using classic:", ex)
    return ClassicScene()
