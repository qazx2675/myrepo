package main

// stress-test -yaml-state: 인벤토리 소스의 현재 yml 과 인벤토리 호스트 이름을 읽어 출력한다 (읽기 전용 — GET 만 쓴다).
//
//	stress-test -yaml-state [-user <u> | -conf <파일>]
//
// new_setup -run -auto 가 launch 직전에 부른다. 다른 사람이 같은 개인 인벤토리의 yml 을 바꾸거나 호스트를 바꿨는지 알려고
// 우리가 마지막에 등록한 값(아래 [등록] 줄)과 비교한다. 출력은 줄마다 "[yaml상태] key=value":
//
//	[yaml상태] yml=<소스의 source_field 값(보통 source_path)>
//	[yaml상태] source=<소스 ID> inventory=<인벤토리 ID>
//	[yaml상태] count=<호스트 수>
//	[yaml상태] hosts=<호스트 이름을 쉼표로 이은 것(정렬)>
//
// 종료코드: 0 정상, 2 설정·접속·API 오류. 등록(register)이 끝나면 같은 형식의 한 줄을 낸다:
//
//	[등록] yml=<등록한 yml> hosts=<등록 직후 인벤토리 호스트 이름(쉼표)>   (호스트를 못 읽으면 hosts= 없이 yml= 만)

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	yamlStateMark   = "[yaml상태] "
	registeredMark  = "[등록] "
	excludedMark    = "[제외] "   // -batch: stage=<a|b> hosts=<h1,h2> reason=<접속불가|DNS실패>
	jobMark         = "[job] "  // -batch: stage=<a|b> id=<AWX job id>
	resultMark      = "[결과] "   // -batch: stage=<a|b> done=<csv> notrun=<csv> nomon=<csv>
	deleteCheckMark = "[삭제확인] " // -batch: before=<삭제 전 대수> after=<삭제 뒤 대수> (inventory_match_exclude 호스트 제외)
)

// yamlState 는 인벤토리 소스의 현재 모습이다.
type yamlState struct {
	SourceID  int
	Yml       string // 소스의 source_field 값
	Inventory int    // 소스가 속한 인벤토리(invsync 가 채우는 곳)
	Hosts     []string
}

// inventoryHosts 는 인벤토리의 호스트 이름 목록(정렬)이다. next 페이지를 끝까지 따라간다.
func (c *awxClient) inventoryHosts(invID int) ([]string, error) {
	var names []string
	path := fmt.Sprintf("/api/v2/inventories/%d/hosts/?page_size=200", invID)
	for path != "" {
		var l struct {
			Next    string `json:"next"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		if err := c.do(http.MethodGet, path, nil, &l); err != nil {
			return nil, fmt.Errorf("인벤토리(%d)의 호스트 조회 실패: %w", invID, err)
		}
		for _, r := range l.Results {
			names = append(names, r.Name)
		}
		path = ""
		if strings.HasPrefix(l.Next, "/") {
			path = l.Next
		}
	}
	sort.Strings(names)
	return names, nil
}

// yamlState 는 invsync 가 쓰는 인벤토리 소스(inventorySourceID)의 yml 과 그 인벤토리의 호스트 이름을 읽는다.
func (c *awxClient) yamlState(cfg *Config) (yamlState, error) {
	srcID, err := c.inventorySourceID(cfg)
	if err != nil {
		return yamlState{}, err
	}
	src, err := c.getSource(srcID)
	if err != nil {
		return yamlState{}, fmt.Errorf("인벤토리 소스(%d) 조회 실패: %w", srcID, err)
	}
	inv := numField(src, "inventory")
	if inv == 0 {
		return yamlState{}, fmt.Errorf("인벤토리 소스(%d)가 속한 인벤토리를 알 수 없습니다", srcID)
	}
	hosts, err := c.inventoryHosts(inv)
	if err != nil {
		return yamlState{}, err
	}
	return yamlState{SourceID: srcID, Yml: valText(src[cfg.SourceField]), Inventory: inv, Hosts: hosts}, nil
}

// registeredLine 은 등록이 끝났을 때 내는 [등록] 줄이다. hosts 가 nil 이면 hosts= 를 붙이지 않는다(읽지 못함).
func registeredLine(yml string, hosts []string) string {
	s := registeredMark + "yml=" + yml
	if hosts != nil {
		s += " hosts=" + strings.Join(hosts, ",")
	}
	return s
}

// yamlStateRun 은 -yaml-state 본체다. 출력은 w, 종료코드는 0/2.
func yamlStateRun(w io.Writer, cfg *Config, client *awxClient) int {
	st, err := client.yamlState(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return 2
	}
	fmt.Fprintf(w, "%syml=%s\n", yamlStateMark, st.Yml)
	fmt.Fprintf(w, "%ssource=%d inventory=%d\n", yamlStateMark, st.SourceID, st.Inventory)
	fmt.Fprintf(w, "%scount=%d\n", yamlStateMark, len(st.Hosts))
	fmt.Fprintf(w, "%shosts=%s\n", yamlStateMark, strings.Join(st.Hosts, ","))
	return 0
}

// runYamlState 는 conf 를 찾아 읽고 AWX 접속·소스 정보만 확인한 뒤 yamlStateRun 을 부른다.
func runYamlState(userFlag, confFlag string) int {
	user := resolveUser(userFlag)
	if confFlag == "" && (user == "" || !userRe.MatchString(user)) {
		return fail2("사용자를 정할 수 없거나 형식이 올바르지 않습니다 (%q). -user, STRESS_USER 또는 -conf 를 확인하세요", user)
	}
	confPath, err := resolveConfPath(confFlag, user)
	if err != nil {
		return fail2("%v", err)
	}
	cfg, err := loadConfig(confPath)
	if err != nil {
		return fail2("%v", err)
	}
	var miss []string
	for _, k := range []struct{ name, v string }{{"awx_url", cfg.AWXURL}, {"username", cfg.Username}, {"password", cfg.Password}} {
		if strings.TrimSpace(k.v) == "" {
			miss = append(miss, k.name)
		}
	}
	if cfg.InventorySource == "" && cfg.Inventory == "" {
		miss = append(miss, "inventory_source 또는 inventory")
	}
	if len(miss) > 0 {
		return fail2("설정 파일(%s)에 비어 있는 값이 있습니다: %s", confPath, strings.Join(miss, ", "))
	}
	return yamlStateRun(os.Stdout, cfg, newAWXClient(cfg.AWXURL, cfg.Username, cfg.Password, cfg.InsecureTLS, 10*time.Second))
}
