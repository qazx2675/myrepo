package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

func main() {
	vc := flag.String("vc", "", "vCenter address (e.g. 192.168.0.50)")
	id := flag.String("id", "", "vCenter login id (e.g. administrator@vsphere.local)")
	f := flag.String("f", "", "path to worklist file: target VM names, one per line")
	insecure := flag.Bool("insecure", true, "skip TLS certificate verification")
	flag.Parse()

	if *vc == "" || *id == "" || *f == "" {
		fmt.Fprintln(os.Stderr, "usage: vm-shares-normal -vc <vcenter> -id <user> -f <worklist file>")
		fmt.Fprintln(os.Stderr, "password is read from the VC_PASSWORD environment variable")
		os.Exit(1)
	}

	pass := os.Getenv("VC_PASSWORD")
	if pass == "" {
		fmt.Fprintln(os.Stderr, "VC_PASSWORD environment variable is required")
		os.Exit(1)
	}

	names, err := readWorklist(*f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read worklist: %v\n", err)
		os.Exit(1)
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "worklist file has no VM names")
		os.Exit(1)
	}

	ctx := context.Background()

	u := &url.URL{Scheme: "https", Host: *vc, Path: "/sdk"}
	u.User = url.UserPassword(*id, pass)

	client, err := govmomi.NewClient(ctx, u, *insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to vCenter: %v\n", err)
		os.Exit(1)
	}
	defer client.Logout(ctx)

	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create container view: %v\n", err)
		os.Exit(1)
	}
	defer cv.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &vms); err != nil {
		fmt.Fprintf(os.Stderr, "failed to retrieve VM list: %v\n", err)
		os.Exit(1)
	}

	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}

	found := make(map[string]bool, len(names))

	for _, vm := range vms {
		if !want[vm.Name] {
			continue
		}
		found[vm.Name] = true

		spec := types.VirtualMachineConfigSpec{
			CpuAllocation: &types.ResourceAllocationInfo{
				Shares: &types.SharesInfo{Level: types.SharesLevelNormal},
			},
			MemoryAllocation: &types.ResourceAllocationInfo{
				Shares: &types.SharesInfo{Level: types.SharesLevelNormal},
			},
		}

		ref := object.NewVirtualMachine(client.Client, vm.Reference())
		task, err := ref.Reconfigure(ctx, spec)
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", vm.Name, err)
			continue
		}
		if err := task.Wait(ctx); err != nil {
			fmt.Printf("[FAIL] %s: %v\n", vm.Name, err)
			continue
		}
		fmt.Printf("[OK]   %s: CPU/Memory shares ratio set to normal\n", vm.Name)
	}

	for _, n := range names {
		if !found[n] {
			fmt.Printf("[SKIP] %s: not found in vCenter\n", n)
		}
	}
}

func readWorklist(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var names []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	return names, scanner.Err()
}
