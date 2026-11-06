package main

// 템플릿 인벤토리 바꾸기·되돌리기: default_inventory → username 인벤토리 → launch → 곧바로 default_inventory.
// AWX 는 job 을 만들 때 인벤토리를 고정하므로 launch 응답 뒤 바로 되돌려도 실행에는 영향이 없다.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
)

// busyPrefix 는 -batch 에서 템플릿 인벤토리가 사용 중일 때 stdout 에 찍는 줄의 머리다.
// 호출자(setup_check -run)는 이 줄을 보고 stdin 으로 "r"(다시 확인) 또는 그 밖(중단)을 한 줄 넣는다.
const busyPrefix = "[인벤토리사용중] "

// errBusy 는 템플릿 인벤토리가 사용 중이라 묶음을 중단했을 때 쓴다.
var errBusy = errors.New("템플릿 인벤토리 사용 중")

// invSwitch 는 지금 username 인벤토리로 바꿔 둔 템플릿(되돌려야 할 것)을 기억한다. 신호 처리기와 함께 쓴다.
type invSwitch struct {
	mu     sync.Mutex
	active bool
	client *awxClient
	tmplID int
	defID  int
}

// curSwitch 는 프로세스 전체에서 하나다 (신호를 받으면 이것을 되돌린다).
var curSwitch = &invSwitch{}

// begin 은 PATCH 전에 부른다 (PATCH 직후 신호가 와도 되돌리도록).
func (s *invSwitch) begin(c *awxClient, tmplID, defID int) {
	s.mu.Lock()
	s.active, s.client, s.tmplID, s.defID = true, c, tmplID, defID
	s.mu.Unlock()
}

// cancel 은 되돌리지 않고 잊는다 (다른 사람이 이미 다른 인벤토리로 바꾼 경우).
func (s *invSwitch) cancel() {
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
}

// restore 는 바꿔 둔 템플릿이 있으면 default 인벤토리로 되돌린다.
func (s *invSwitch) restore() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restoreLocked()
}

func (s *invSwitch) restoreLocked() error {
	if !s.active {
		return nil
	}
	s.active = false
	return s.client.setTemplateInventory(s.tmplID, s.defID)
}

// exitFn 은 신호 처리 뒤 종료 함수다 (테스트에서 바꾼다).
var exitFn = os.Exit

// onSignal 은 SIGINT/SIGTERM 을 받았을 때 부른다: 바꿔 둔 인벤토리가 있으면 되돌린 뒤 128+신호번호로 종료.
// 잠금을 쥔 채 종료하므로 본 흐름이 동시에 되돌리거나 새로 바꾸지 못한다.
func (s *invSwitch) onSignal(sig os.Signal, w io.Writer) {
	s.mu.Lock()
	if s.active {
		fmt.Fprintf(w, "\n%s 신호(%v)를 받아 템플릿(%d) 인벤토리를 되돌립니다...\n", markWarn, sig, s.tmplID)
		tmplID := s.tmplID
		if err := s.restoreLocked(); err != nil {
			fmt.Fprintf(w, "%s ✗✗ 템플릿(%d) 인벤토리 복원 실패: %v — AWX 에서 수동으로 되돌리세요\n", markFail, tmplID, err)
		} else {
			fmt.Fprintf(w, "%s 템플릿(%d) 인벤토리를 되돌렸습니다\n", markOK, tmplID)
		}
	}
	code := 130
	if n, ok := sig.(syscall.Signal); ok {
		code = 128 + int(n)
	}
	exitFn(code)
}

// busyMessage 는 템플릿 인벤토리가 default 가 아닐 때 보여 줄 문장이다.
func busyMessage(t *jobTemplate, def string) string {
	return fmt.Sprintf("템플릿 %s(%d) 인벤토리가 %s 입니다(%s 아님) — 그 인벤토리 사용자에게 %s 로 바꿔 달라고 요청하세요", t.Name, t.ID, t.invName(), def, def)
}

// askBusy 는 사용 중일 때 다시 확인할지 묻는다. true = 다시 확인(r).
// 대화형은 프롬프트, 그 밖(-batch 등)은 stdout 에 busyPrefix 줄을 찍고 stdin 에서 한 줄을 읽는다(EOF = 중단).
func (a *app) askBusy(msg string) bool {
	if a.interactive {
		a.logf("%s %s", markWarn, msg)
		fmt.Fprint(a.out, "r=다시 확인, x=이 묶음 중단: ")
	} else {
		fmt.Fprintln(a.out, busyPrefix+msg) // os.Stdout 은 버퍼가 없어 바로 나간다
	}
	if a.in == nil {
		return false
	}
	line, _ := a.in.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "r"
}

// launchSwitched 는 템플릿 인벤토리를 확인·변경하고 launch 한 뒤 곧바로 되돌린다.
// 돌려주는 restoreErr 가 nil 이 아니면 되돌리기에 실패한 것이다(job 은 이미 실행됐을 수 있음).
func (a *app) launchSwitched(tmplID int, vars map[string]interface{}) (job int, restoreErr, err error) {
	def := a.cfg.DefaultInventory
	for {
		t, err := a.client.getTemplate(tmplID)
		if err != nil {
			return 0, nil, fmt.Errorf("템플릿 조회 실패: %w", err)
		}
		if t.invName() != def {
			a.appendBusyHistory(t)
			if a.askBusy(busyMessage(t, def)) {
				continue
			}
			return 0, nil, errBusy
		}
		curSwitch.begin(a.client, t.ID, a.defInvID)
		if err := a.client.setTemplateInventory(t.ID, a.userInvID); err != nil {
			return 0, curSwitch.restore(), fmt.Errorf("템플릿 인벤토리를 %s 로 바꾸지 못했습니다: %w", a.cfg.Username, err)
		}
		v, err := a.client.getTemplate(t.ID)
		if err != nil {
			return 0, curSwitch.restore(), fmt.Errorf("템플릿 인벤토리 변경 확인 실패: %w", err)
		}
		if v.Inventory != a.userInvID {
			// 다른 사람과 경합: 지금 인벤토리는 우리 것이 아니므로 되돌리지 않는다
			curSwitch.cancel()
			if v.invName() == def {
				return 0, nil, fmt.Errorf("템플릿 인벤토리를 %s 로 바꿨지만 반영되지 않았습니다(%s 그대로)", a.cfg.Username, def)
			}
			a.appendBusyHistory(v)
			if a.askBusy(busyMessage(v, def)) {
				continue
			}
			return 0, nil, errBusy
		}
		a.logf("%s 템플릿 %s(%d) 인벤토리 %s → %s", markInfo, t.Name, t.ID, def, a.cfg.Username)
		job, lerr := a.client.launch(t.ID, vars)
		rerr := curSwitch.restore()
		if rerr == nil {
			a.logf("%s 템플릿 %s(%d) 인벤토리 %s 로 되돌림", markInfo, t.Name, t.ID, def)
		}
		return job, rerr, lerr
	}
}

func (a *app) appendBusyHistory(t *jobTemplate) {
	appendHistory(a.cfg, fmt.Sprintf("user=%s action=invcheck template=%d inventory=%q status=busy", a.user, t.ID, t.invName()))
}

// prepareAWX 는 단계 시작 전에 필요한 템플릿과 인벤토리(username, default_inventory)가 있는지 확인하고 ID 를 기억한다.
func (a *app) prepareAWX(steps []string) error {
	a.tmpl = map[string]*jobTemplate{}
	for _, s := range steps {
		tc := a.templateConf(s)
		t, err := a.client.resolveTemplate(tc.Template)
		if err != nil {
			return fmt.Errorf("%s 템플릿(%s) 확인 실패: %v", stageLabel(s), tc.Template, err)
		}
		a.tmpl[s] = t
	}
	var err error
	if a.userInvID, err = a.client.findInventory(a.cfg.Username); err != nil {
		return fmt.Errorf("%v (템플릿 실행 때 바꿀 인벤토리 = conf 의 username)", err)
	}
	if a.defInvID, err = a.client.findInventory(a.cfg.DefaultInventory); err != nil {
		return fmt.Errorf("%v (conf 의 default_inventory)", err)
	}
	a.logf("%s AWX 확인: 인벤토리 %s(%d) ↔ %s(%d)", markInfo, a.cfg.DefaultInventory, a.defInvID, a.cfg.Username, a.userInvID)
	return nil
}

func (a *app) templateConf(stage string) TemplateConf {
	if stage == stageStress {
		return a.cfg.Stress
	}
	return a.cfg.CPU
}
