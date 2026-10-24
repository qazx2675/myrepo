package main

import (
	"os"
	"testing"

	"github.com/tc-hib/winres"
)

func TestGenerateSyso(t *testing.T) {
	icoFile, err := os.Open("../../icon.ico")
	if err != nil {
		t.Skip("icon.ico not found, skipping syso generation")
		return
	}
	defer icoFile.Close()

	icon, err := winres.LoadICO(icoFile)
	if err != nil {
		t.Fatalf("failed to parse icon.ico: %v", err)
	}

	rs := winres.ResourceSet{}
	rs.SetIcon(winres.ID(1), icon)

	out, err := os.Create("rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatalf("failed to create rsrc_windows_amd64.syso: %v", err)
	}
	defer out.Close()

	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		t.Fatalf("failed to write syso object: %v", err)
	}
}
