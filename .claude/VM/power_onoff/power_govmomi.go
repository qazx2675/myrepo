package main

import "context"

// govmomiOps 는 VMOps 의 실제 구현이다(vCenter 로 전원 태스크를 보낸다).
type govmomiOps struct{ c *Client }

func NewGovmomiOps(c *Client) VMOps { return &govmomiOps{c: c} }

func (g *govmomiOps) PowerOn(ctx context.Context, vm *VMInfo) error {
	t, err := vm.Obj.PowerOn(ctx)
	if err != nil {
		return err
	}
	return t.Wait(ctx)
}

// ShutdownGuest 는 요청만 보낸다(Guest OS 가 꺼지는 것은 Refresh 로 확인).
func (g *govmomiOps) ShutdownGuest(ctx context.Context, vm *VMInfo) error {
	return vm.Obj.ShutdownGuest(ctx)
}

func (g *govmomiOps) PowerOff(ctx context.Context, vm *VMInfo) error {
	t, err := vm.Obj.PowerOff(ctx)
	if err != nil {
		return err
	}
	return t.Wait(ctx)
}

func (g *govmomiOps) Refresh(ctx context.Context, vm *VMInfo) error { return g.c.Refresh(ctx, vm) }
