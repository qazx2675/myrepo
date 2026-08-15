package vcsimtest

import (
	"context"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// vmWrapper는 vcsim 테스트에서 VirtualMachine 오브젝트를 다루기 위한 래퍼다.
type vmWrapper struct {
	obj *object.VirtualMachine
}

// newVMObject는 ManagedObjectReference로부터 govmomi VirtualMachine 오브젝트를 생성한다.
func newVMObject(client *govmomi.Client, ref types.ManagedObjectReference) *vmWrapper {
	return &vmWrapper{obj: object.NewVirtualMachine(client.Client, ref)}
}

// Reconfigure는 VirtualMachine에 ConfigSpec을 적용하는 task를 반환한다.
func (v *vmWrapper) Reconfigure(ctx context.Context, spec types.VirtualMachineConfigSpec) (*object.Task, error) {
	return v.obj.Reconfigure(ctx, spec)
}
