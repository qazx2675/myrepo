package webui

import (
	"io/fs"
	"testing"
)

func TestEmbeddedFrontendAssets(t *testing.T) {
	sub, err := FS()
	if err != nil {
		t.Fatalf("FS() failed: %v", err)
	}

	requiredFiles := []string{
		"index.html",
		"vendor/three.min.js",
		"vendor/OrbitControls.js",
		"vendor/CSS2DRenderer.js",
		"vendor/papaparse.min.js",
		"js/renderer3d/sceneManager.js",
		"js/renderer3d/rackMeshBuilder.js",
		"js/renderer3d/deviceMeshBuilder.js",
		"js/renderer3d/textureGenerator.js",
		"js/ui/sharedFolderModal.js",
		"js/ui/topToolbar.js",
	}

	for _, name := range requiredFiles {
		data, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Errorf("missing embedded asset %q: %v", name, err)
			continue
		}
		if len(data) < 100 {
			t.Errorf("embedded asset %q is unexpectedly small (%d bytes)", name, len(data))
		}
	}
}
