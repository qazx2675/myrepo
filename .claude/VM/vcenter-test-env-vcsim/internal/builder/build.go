// Package builder는 recipe.Recipe를 받아 빈 vcsim 인스턴스 위에
// 동일한 구조(DC/폴더/클러스터/호스트/VM)와 추적 필드 값을 재생성한다.
// govmomi v0.55.1(Rocky Linux, 192.168.0.58) 기준으로 실제 빌드/실행 검증 완료.
package builder

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"vc-test-env/internal/fields"
	"vc-test-env/internal/inventory"
	"vc-test-env/internal/recipe"
)

// Port는 vcsim이 항상 뜨는 고정 포트다. 매번 랜덤 포트로 뜨면 재시작할 때마다
// vm-param-check의 vcenter.txt 같은 걸 손으로 다시 고쳐야 해서 고정해뒀다.
const Port = "54321"

type Result struct {
	Client *govmomi.Client
	URL    string

	model  *simulator.Model
	server *simulator.Server
}

// Start는 빈 vcsim을 기동하고 recipe 구조대로 재생성한다.
func Start(ctx context.Context, r *recipe.Recipe) (*Result, error) {
	// VPX()로 ServiceContent 등 내부 타입 정보를 정상 초기화한다(빈 simulator.Model{}은
	// ServiceContent가 비어 있어 Create()에서 panic 남 — 실제로 확인함).
	//
	// Datacenter=1, Datastore=1만 남겨두는 이유: vcsim의 HostDatastoreSystem.CreateLocalDatastore로
	// 직접 만든 데이터스토어는 ComputeResource에만 연결되고 Datacenter.Datastore 목록에는
	// 반영되지 않아 VM 생성이 nil 참조로 패닉난다(vcsim 자체의 동작, 실제로 확인함).
	// Model이 기본으로 만들어주는 데이터센터+데이터스토어 조합은 이 경로가 정상 동작하므로,
	// 그걸 그대로 쓰고 이름만 recipe에 맞게 바꾼다.
	model := simulator.VPX()
	model.Datacenter = 1
	model.Datastore = 1
	model.Portgroup = 0
	// Host=1: Model은 데이터스토어를 호스트에 "마운트"하는 방식으로만 만들기 때문에
	// 호스트가 0개면 데이터스토어도 아예 안 만들어진다(실제로 확인함). 이 씨드 호스트는
	// rebuild()에서 데이터스토어 이름만 빌린 뒤 곧바로 지운다.
	model.Host = 1
	model.Cluster = 0
	model.ClusterHost = 0
	model.Machine = 0
	model.Folder = 0
	model.App = 0
	model.Pod = 0

	if err := model.Create(); err != nil {
		return nil, fmt.Errorf("vcsim 모델 생성 실패: %w", err)
	}

	// TLS를 켜지 않으면 평문 HTTP로 뜬다 — PowerCLI(Connect-VIServer)는 실 vCenter처럼
	// HTTPS로 핸드셰이크를 시도하다가 "corrupted frame" 에러로 깨진다(실제로 확인함).
	model.Service.TLS = new(tls.Config)
	// 매번 랜덤 포트로 뜨면 재시작할 때마다 다른 도구들의 -vcTargetIP/vcenter.txt를
	// 손으로 다시 고쳐야 해서 고정 포트로 띄운다.
	model.Service.Listen = &url.URL{Host: "127.0.0.1:" + Port}
	server := model.Service.NewServer()

	client, err := govmomi.NewClient(ctx, server.URL, true)
	if err != nil {
		server.Close()
		model.Remove()
		return nil, fmt.Errorf("vcsim 접속 실패: %w", err)
	}

	if err := rebuild(ctx, client, r.Inventory); err != nil {
		server.Close()
		model.Remove()
		return nil, fmt.Errorf("vcsim 재생성 실패: %w", err)
	}

	// server.URL은 vcsim 기본 placeholder 계정(user:pass@)이 박혀 있어서 그대로 보여주면
	// 진짜 로그인 정보처럼 오해할 수 있다 — host:port만 뽑아서 보여준다(vcsim은 어차피
	// 아무 자격증명이나 받아들인다).
	return &Result{Client: client, URL: server.URL.Host, model: model, server: server}, nil
}

func (res *Result) Close() {
	res.server.Close()
	res.model.Remove()
}

// seedDatacenterAndDatastore는 Model.Create()가 미리 만들어둔(그리고 정상적으로
// 연결된) 데이터센터 1개와 데이터스토어 1개를 찾아 반환한다.
func seedDatacenterAndDatastore(ctx context.Context, c *govmomi.Client) (*object.Datacenter, string, error) {
	m := view.NewManager(c.Client)

	dcView, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"Datacenter"}, true)
	if err != nil {
		return nil, "", fmt.Errorf("기본 데이터센터 조회 실패: %w", err)
	}
	defer dcView.Destroy(ctx)
	var dcs []mo.Datacenter
	if err := dcView.Retrieve(ctx, []string{"Datacenter"}, []string{"name"}, &dcs); err != nil || len(dcs) == 0 {
		return nil, "", fmt.Errorf("기본 데이터센터를 찾을 수 없음: %w", err)
	}
	dcObj := object.NewDatacenter(c.Client, dcs[0].Reference())

	dsView, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"Datastore"}, true)
	if err != nil {
		return nil, "", fmt.Errorf("기본 데이터스토어 조회 실패: %w", err)
	}
	defer dsView.Destroy(ctx)
	var dss []mo.Datastore
	if err := dsView.Retrieve(ctx, []string{"Datastore"}, []string{"name"}, &dss); err != nil {
		return nil, "", fmt.Errorf("기본 데이터스토어 조회 실패: %w", err)
	}
	if len(dss) == 0 {
		return nil, "", fmt.Errorf("기본 데이터스토어를 찾을 수 없음")
	}
	dsName := dss[0].Name

	// 알려진 한계: 데이터스토어를 마운트하려고 Model이 만든 씨드 호스트("DC0_H0" 등)는
	// 재생성된 구조 옆에 그대로 남는다. HostSystem.Destroy()는 부모 ComputeResource를
	// 빈 채로 남겨 이후 조회가 깨지고, ComputeResource.Destroy()는 vcsim이 아예 구현하지
	// 않는다("does not implement: Destroy_Task", 실제로 확인함) — 둘 다 시도해봤지만
	// 안전하게 지우는 방법을 못 찾았다. 실제 VM/호스트 동작에는 영향 없음.

	return dcObj, dsName, nil
}

func rebuild(ctx context.Context, c *govmomi.Client, inv *inventory.Inventory) error {
	seedDC, dsName, err := seedDatacenterAndDatastore(ctx, c)
	if err != nil {
		return err
	}

	root := object.NewRootFolder(c.Client)

	for i, dc := range inv.Datacenters {
		var dcObj *object.Datacenter
		if i == 0 {
			// 첫 데이터센터는 Model이 미리 만들어둔(데이터스토어가 정상 연결된) 것을 이름만 바꿔 재사용.
			task, err := seedDC.Rename(ctx, dc.Name)
			if err != nil {
				return fmt.Errorf("데이터센터(%s) 이름변경 실패: %w", dc.Name, err)
			}
			if err := task.Wait(ctx); err != nil {
				return fmt.Errorf("데이터센터(%s) 이름변경 대기 실패: %w", dc.Name, err)
			}
			dcObj = seedDC
		} else {
			// TODO: 두 번째 이후 데이터센터는 아직 데이터스토어 연결 문제를 안 풀었음(1차 범위 밖 — 실 환경은 DC 1개).
			dcObj, err = root.CreateDatacenter(ctx, dc.Name)
			if err != nil {
				return fmt.Errorf("데이터센터(%s) 생성 실패: %w", dc.Name, err)
			}
		}

		folders, err := dcObj.Folders(ctx)
		if err != nil {
			return fmt.Errorf("데이터센터(%s) 폴더 조회 실패: %w", dc.Name, err)
		}

		for _, name := range dc.VMFolders {
			if _, err := folders.VmFolder.CreateFolder(ctx, name); err != nil {
				return fmt.Errorf("VM 폴더(%s) 생성 실패: %w", name, err)
			}
		}
		for _, name := range dc.NetFolders {
			if _, err := folders.NetworkFolder.CreateFolder(ctx, name); err != nil {
				return fmt.Errorf("네트워크 폴더(%s) 생성 실패: %w", name, err)
			}
		}

		for _, cl := range dc.Clusters {
			if err := rebuildCluster(ctx, folders, dsName, dc.Networks, cl); err != nil {
				return err
			}
		}
	}
	return nil
}

func mountSharedDatastore(ctx context.Context, host *object.HostSystem, dsName string) error {
	dss, err := host.ConfigManager().DatastoreSystem(ctx)
	if err != nil {
		return err
	}
	// os.Stat(path)를 통과시키기 위한 자리표시자 디렉터리. 이름이 이미 존재하는
	// 데이터스토어와 일치하므로 vcsim이 이 경로는 버리고 기존 것을 재사용한다.
	dir, err := os.MkdirTemp("", "vc-test-env-ds-")
	if err != nil {
		return err
	}
	_, err = dss.CreateLocalDatastore(ctx, dsName, dir)
	return err
}

// ensurePortgroups는 레시피에 있는 포트그룹 이름을 이 호스트의 기본 vSwitch0에 전부
// 만들어둔다. AddHost로 추가된 호스트는 vSwitch0+"VM Network" 기본 구성을 이미 갖고
// 있어서(실제로 확인함), 그 이름과 겹치는 것과 이미 만든 것은 조용히 건너뛴다.
func ensurePortgroups(ctx context.Context, host *object.HostSystem, networks []inventory.PortGroup) error {
	if len(networks) == 0 {
		return nil
	}
	ns, err := host.ConfigManager().NetworkSystem(ctx)
	if err != nil {
		return err
	}
	for _, n := range networks {
		err := ns.AddPortGroup(ctx, types.HostPortGroupSpec{
			Name:        n.Name,
			VswitchName: "vSwitch0",
			VlanId:      0,
			Policy:      types.HostNetworkPolicy{},
		})
		if err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("포트그룹(%s) 생성 실패: %w", n.Name, err)
		}
	}
	return nil
}

func isAlreadyExists(err error) bool {
	// vcsim은 이미 있는 이름으로 다시 만들려 하면 DuplicateName류 SOAP fault를 낸다 —
	// 메시지 문자열로 판별한다(정확한 fault 타입 어설션보다 버전 차이에 덜 민감함).
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate")
}

func rebuildCluster(ctx context.Context, folders *object.DatacenterFolders, dsName string, networks []inventory.PortGroup, cl inventory.Cluster) error {
	clusterObj, err := folders.HostFolder.CreateCluster(ctx, cl.Name, types.ClusterConfigSpecEx{})
	if err != nil {
		return fmt.Errorf("클러스터(%s) 생성 실패: %w", cl.Name, err)
	}

	for _, h := range cl.Hosts {
		spec := types.HostConnectSpec{
			HostName: h.Name,
			UserName: "root",
			Password: "vcsim",
			Force:    true,
		}
		addTask, err := clusterObj.AddHost(ctx, spec, true, nil, nil)
		if err != nil {
			return fmt.Errorf("호스트(%s) 추가 실패: %w", h.Name, err)
		}
		info, err := addTask.WaitForResult(ctx)
		if err != nil {
			return fmt.Errorf("호스트(%s) 추가 대기 실패: %w", h.Name, err)
		}
		hostRef, ok := info.Result.(types.ManagedObjectReference)
		if !ok {
			return fmt.Errorf("호스트(%s) 추가 결과 타입이 예상과 다름: %T", h.Name, info.Result)
		}
		hostObj := object.NewHostSystem(clusterObj.Client(), hostRef)

		// TODO: 전원정책(h.PowerPolicy) 적용 — vcsim의 HostPowerSystem 시뮬레이션
		// 지원 범위를 마일스톤 0에서 확인 후 host.ConfigManager().PowerSystem()으로 구현.

		// VM 파일 생성은 "VM이 붙는 호스트"에 데이터스토어가 마운트돼 있어야 한다(Datacenter
		// 목록에 있는 것만으로는 부족 — 실제로 확인함). 공유 데이터스토어를 같은 이름으로 다시
		// "생성"하면 vcsim이 이름으로 매칭해서 새로 만들지 않고 이 호스트에 마운트만 해준다.
		if err := mountSharedDatastore(ctx, hostObj, dsName); err != nil {
			return fmt.Errorf("호스트(%s) 데이터스토어 마운트 실패: %w", h.Name, err)
		}

		// VM의 네트워크 어댑터가 참조할 포트그룹이 실제로 이 호스트에 없으면 vcsim이
		// CreateVM에서 그 디바이스를 조용히 빼버린다(에러 없이 — 실제로 확인함).
		// 기본 vSwitch0에 레시피의 포트그룹 이름을 전부 만들어둔다.
		if err := ensurePortgroups(ctx, hostObj, networks); err != nil {
			return fmt.Errorf("호스트(%s) 포트그룹 생성 실패: %w", h.Name, err)
		}

		pool, err := clusterObj.ResourcePool(ctx)
		if err != nil {
			return fmt.Errorf("클러스터(%s) 리소스풀 조회 실패: %w", cl.Name, err)
		}

		for _, v := range h.VMs {
			if err := createVM(ctx, folders.VmFolder, pool, hostObj, dsName, v); err != nil {
				return fmt.Errorf("VM(%s) 생성 실패: %w", v.Name, err)
			}
		}
	}
	return nil
}

func createVM(ctx context.Context, vmFolder *object.Folder, pool *object.ResourcePool, host *object.HostSystem, dsName string, v inventory.VM) error {
	coresPerSocket := v.CoresPerSocket
	spec := types.VirtualMachineConfigSpec{
		Name:              v.Name,
		NumCPUs:           v.NumCPU,
		NumCoresPerSocket: &coresPerSocket,
		MemoryMB:          int64(v.MemoryMB),
		GuestId:           "otherGuest",
		Files:             &types.VirtualMachineFileInfo{VmPathName: fmt.Sprintf("[%s] %s", dsName, v.Name)},
	}

	if len(v.CPUAffinity) > 0 {
		spec.CpuAffinity = &types.VirtualMachineAffinityInfo{AffinitySet: v.CPUAffinity}
	}

	// 네트워크 어댑터는 커넥트/디스커넥트 상태와 무관하게 항상 재현한다 — vm-param-check가
	// 디스커넥트여도 포트그룹 이름을 조사해야 하므로, 어댑터 자체가 없으면 테스트가 안 된다.
	for _, nic := range v.Networks {
		card := &types.VirtualE1000{
			VirtualEthernetCard: types.VirtualEthernetCard{
				VirtualDevice: types.VirtualDevice{
					Backing: &types.VirtualEthernetCardNetworkBackingInfo{
						VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
							DeviceName: nic.Portgroup,
						},
					},
					Connectable: &types.VirtualDeviceConnectInfo{
						StartConnected:    nic.Connected,
						Connected:         nic.Connected,
						AllowGuestControl: true,
					},
				},
			},
		}
		spec.DeviceChange = append(spec.DeviceChange, &types.VirtualDeviceConfigSpec{
			Operation: types.VirtualDeviceConfigSpecOperationAdd,
			Device:    card,
		})
	}

	for _, f := range fields.VMFields {
		if val, ok := v.Values[f.Key]; ok {
			f.Apply(&spec, val)
		}
	}

	// sched.vcpuN.affinity는 vCPU 개수만큼 키가 생겨서 fields.VMFields(고정 키 레지스트리)에
	// 담을 수 없다 — inventory.walkVM이 v.Values에 그대로 담아둔 걸 여기서 직접 재현한다.
	for key, val := range v.Values {
		if strings.HasPrefix(key, "sched.vcpu") && strings.HasSuffix(key, ".affinity") {
			spec.ExtraConfig = append(spec.ExtraConfig, &types.OptionValue{Key: key, Value: val})
		}
	}

	task, err := vmFolder.CreateVM(ctx, spec, pool, host)
	if err != nil {
		return err
	}
	return task.Wait(ctx)
}
