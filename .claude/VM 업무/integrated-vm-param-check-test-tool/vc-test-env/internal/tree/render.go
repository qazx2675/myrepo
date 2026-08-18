// Package tree는 inventory.Inventory를 터미널 ASCII 트리로 출력한다.
package tree

import (
	"fmt"
	"io"

	"vc-test-env/internal/inventory"
)

func Render(w io.Writer, inv *inventory.Inventory) {
	for _, dc := range inv.Datacenters {
		fmt.Fprintf(w, "Datacenter: %s\n", dc.Name)

		if len(dc.VMFolders) > 0 {
			fmt.Fprintln(w, "├── VM Folders")
			for i, f := range dc.VMFolders {
				fmt.Fprintf(w, "│   %s %s\n", branch(i, len(dc.VMFolders)), f)
			}
		}
		if len(dc.NetFolders) > 0 {
			fmt.Fprintln(w, "├── Network Folders")
			for i, f := range dc.NetFolders {
				fmt.Fprintf(w, "│   %s %s\n", branch(i, len(dc.NetFolders)), f)
			}
		}

		fmt.Fprintln(w, "├── Clusters")
		for ci, cl := range dc.Clusters {
			fmt.Fprintf(w, "│   %s %s\n", branch(ci, len(dc.Clusters)), cl.Name)
			for hi, h := range cl.Hosts {
				prefix := "│   │   "
				if ci == len(dc.Clusters)-1 {
					prefix = "│       "
				}
				fmt.Fprintf(w, "%s%s %s", prefix, branch(hi, len(cl.Hosts)), h.Name)
				if h.PowerPolicy != "" {
					fmt.Fprintf(w, "  [power=%s]", h.PowerPolicy)
				}
				fmt.Fprintln(w)
				for vi, v := range h.VMs {
					vprefix := prefix + "    "
					fmt.Fprintf(w, "%s%s %s  [cpu=%d cores/socket=%d mem=%dMB]\n",
						vprefix, branch(vi, len(h.VMs)), v.Name, v.NumCPU, v.CoresPerSocket, v.MemoryMB)
				}
			}
		}

		fmt.Fprintln(w, "└── Networks")
		for ni, n := range dc.Networks {
			fmt.Fprintf(w, "    %s %s\n", branch(ni, len(dc.Networks)), n.Name)
		}
	}
}

func branch(i, total int) string {
	if i == total-1 {
		return "└──"
	}
	return "├──"
}
