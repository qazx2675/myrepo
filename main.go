// bios_compare: 폐쇄망에서 실제 수집한 BIOS JSON(out/<일시>/<hostname>.json)의 설정이름과
// bios_json(웹 조사) 의 설정이름을 양방향으로 비교한다. 이름 존재 여부만 본다(값 비교 없음).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type host struct{ name, model string }

type folder struct {
	path, vendor, name string
	aliases            []string
}

var defaultIgnore = []string{
	`(?i)serial`, `(?i)macaddress`, `(?i)(^|_)mac(\d|$)`, `(?i)uuid`, `(?i)servicetag`, `(?i)assettag`,
	`(?i)servername`, `(?i)serverotherinfo`, `(?i)^admin(name|phone|email|other)`, `(?i)ipv[46]`,
	`(?i)ipaddress`, `(?i)^bootseq`, `(?i)bootorder`, `(?i)^nic.*(slot|mac)`,
}

var vendorWords = []string{"proliant", "poweredge", "thinksystem", "synergy", "apollo", "hpe", "dell", "lenovo", "cisco", "ucs"}
var attachRe = regexp.MustCompile(`(?i)^(g\d+|gen\d+|m\d+|v\d+|plus)$`)
var genRe = regexp.MustCompile(`gen(\d)`)
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// canon: 모델 문자열을 비교용 코드로 변환 ("ProLiant DL360 Gen10 Plus" -> "dl360g10plus").
func canon(s string) string {
	s = strings.ToLower(s)
	s = genRe.ReplaceAllString(s, "g$1")
	s = regexp.MustCompile(`^ucs[bcx]-`).ReplaceAllString(strings.TrimSpace(s), "")
	for _, w := range vendorWords {
		s = strings.ReplaceAll(s, w, " ")
	}
	return nonAlnum.ReplaceAllString(s, "")
}

// aliases: 요약 폴더 이름을 모델 단위로 분해한다 (DL360_G9_XL170R_G9 -> dl360g9, xl170rg9).
func aliases(dir string) []string {
	var out, cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, canon(strings.Join(cur, "")))
		}
		cur = nil
	}
	for _, t := range strings.Split(dir, "_") {
		if t == "" || canon(t) == "" {
			continue
		}
		if attachRe.MatchString(t) && len(cur) > 0 {
			cur = append(cur, t)
			continue
		}
		flush()
		cur = []string{t}
	}
	flush()
	return append(out, canon(dir))
}

func norm(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

func readJSON(p string) (map[string]interface{}, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	return m, json.Unmarshal(b, &m)
}

// 실제조사 JSON: systems.*.Bios.Attributes 키와 Model.
func actual(p string) (model string, names []string, err error) {
	m, err := readJSON(p)
	if err != nil {
		return
	}
	sys, _ := m["systems"].(map[string]interface{})
	for _, v := range sys {
		e, _ := v.(map[string]interface{})
		if model == "" {
			model, _ = e["Model"].(string)
		}
		bios, _ := e["Bios"].(map[string]interface{})
		at, _ := bios["Attributes"].(map[string]interface{})
		for k := range at {
			names = append(names, k)
		}
	}
	return
}

// 조사(bios_json) 파일: "attributes" 객체가 있으면 그 키, 없으면 최상위 키(_ 로 시작하는 것 제외).
func researched(dir string) (names []string, found bool) {
	for _, f := range []string{"bios_attributes.json", "bios_tokens.json"} {
		m, err := readJSON(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		found = true
		src := m
		if a, ok := m["attributes"].(map[string]interface{}); ok {
			src = a
		}
		for k := range src {
			if !strings.HasPrefix(k, "_") {
				names = append(names, k)
			}
		}
	}
	return
}

func loadFolders(root string) []folder {
	var fs []folder
	vs, _ := os.ReadDir(root)
	for _, v := range vs {
		if !v.IsDir() {
			continue
		}
		ms, _ := os.ReadDir(filepath.Join(root, v.Name()))
		for _, m := range ms {
			if m.IsDir() {
				fs = append(fs, folder{filepath.Join(root, v.Name(), m.Name()), v.Name(), m.Name(), aliases(m.Name())})
			}
		}
	}
	return fs
}

func find(fs []folder, model string) (string, []folder) {
	c := canon(model)
	var hit []folder
	for _, f := range fs {
		for _, a := range f.aliases {
			if a == c && c != "" {
				hit = append(hit, f)
				break
			}
		}
	}
	return c, hit
}

func loadIgnore(p string) []*regexp.Regexp {
	pats := defaultIgnore
	if f, err := os.Open(p); err == nil {
		defer f.Close()
		pats = nil
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
				pats = append(pats, l)
			}
		}
	}
	var res []*regexp.Regexp
	for _, p := range pats {
		if r, err := regexp.Compile(p); err == nil {
			res = append(res, r)
		} else {
			fmt.Println("[경고] ignore 정규식 오류:", p)
		}
	}
	return res
}

func latest(out string) string {
	es, _ := os.ReadDir(out)
	best := ""
	for _, e := range es {
		if e.IsDir() && e.Name() > best {
			best = e.Name()
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(out, best)
}

func main() {
	list := flag.String("list", "list.txt", "hostname [model] 목록")
	in := flag.String("in", "", "실제조사 JSON 폴더 (기본: ./out 아래 최신 폴더)")
	bj := flag.String("bios-json", "bios_json", "웹 조사(bios_json) 폴더")
	ign := flag.String("ignore", "ignore.txt", "비교 제외 정규식 목록")
	resDir := flag.String("out", "results", "결과 폴더")
	flag.Parse()

	if *in == "" {
		*in = latest("out")
		if *in == "" {
			fmt.Println("실제조사 폴더가 없습니다: ./out/<일시>/ 에 hostname.json 을 넣거나 -in 으로 지정하십시오.")
			os.Exit(1)
		}
	}
	lf, err := os.Open(*list)
	if err != nil {
		fmt.Println("list 열기 실패:", err)
		os.Exit(1)
	}
	var hosts []host
	sc := bufio.NewScanner(lf)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Fields(l)
		h := host{name: f[0]}
		if len(f) > 1 {
			h.model = strings.Join(f[1:], " ")
		}
		hosts = append(hosts, h)
	}
	lf.Close()

	folders := loadFolders(*bj)
	ignores := loadIgnore(*ign)
	ignored := func(n string) bool {
		for _, r := range ignores {
			if r.MatchString(n) {
				return true
			}
		}
		return false
	}

	ts := time.Now().Format("20060102_150405")
	od := filepath.Join(*resDir, ts)
	os.MkdirAll(od, 0755)
	mp, _ := os.Create(filepath.Join(od, "mapping.tsv"))
	fmt.Fprintln(mp, "hostname\tJSON_Model\t판단모델\t모델출처\tbios_json폴더\t상태")

	type group struct {
		label  string
		hosts  []string
		sets   map[string]int
		folder *folder
		status string
		ndup   string
	}
	groups := map[string]*group{}
	var order []string
	var skipped []string

	for _, h := range hosts {
		jm, names, err := actual(filepath.Join(*in, h.name+".json"))
		if err != nil {
			fmt.Fprintf(mp, "%s\t\t\t\t\t실제JSON없음\n", h.name)
			skipped = append(skipped, h.name+"(실제JSON없음)")
			continue
		}
		model, src := h.model, "list.txt"
		if model == "" {
			model, src = jm, "JSON자동"
		}
		if strings.TrimSpace(model) == "" {
			fmt.Fprintf(mp, "%s\t%s\t\t\t\t모델판단불가\n", h.name, jm)
			skipped = append(skipped, h.name+"(모델판단불가: list.txt 에 모델 기재)")
			continue
		}
		label := strings.ReplaceAll(strings.TrimSpace(model), " ", "_")
		g := groups[label]
		if g == nil {
			g = &group{label: label, sets: map[string]int{}}
			_, hit := find(folders, model)
			switch {
			case len(hit) == 0:
				g.status = "없음"
			case len(hit) > 1:
				g.status = "중복매칭"
				g.ndup = hit[1].vendor + "/" + hit[1].name
				g.folder = &hit[0]
			default:
				g.folder = &hit[0]
			}
			groups[label] = g
			order = append(order, label)
		}
		g.hosts = append(g.hosts, h.name)
		for _, n := range names {
			g.sets[n]++
		}
		fn := ""
		if g.folder != nil {
			fn = g.folder.vendor + "/" + g.folder.name
		}
		fmt.Fprintf(mp, "%s\t%s\t%s\t%s\t%s\t%s\n", h.name, jm, label, src, fn, g.status)
	}
	mp.Close()

	sm, _ := os.Create(filepath.Join(od, "summary.txt"))
	w := func(f string, a ...interface{}) { fmt.Fprintf(sm, f, a...); fmt.Printf(f, a...) }
	w("실제조사: %s / 조사자료: %s / 호스트 %d대\n\n", *in, *bj, len(hosts))
	w("모델\t호스트수\tbios_json폴더\t일치\t실제만\t조사만\t제외\t비고\n")
	sort.Strings(order)
	for _, label := range order {
		g := groups[label]
		fn, note := "-", g.status
		var rnames []string
		if g.folder != nil {
			fn = g.folder.vendor + "/" + g.folder.name
			var found bool
			rnames, found = researched(g.folder.path)
			if !found {
				note = "조사파일없음"
			}
		}
		if g.status == "중복매칭" {
			note = "중복매칭(" + g.ndup + " 도 해당, 첫 폴더 사용)"
		}
		if g.status == "없음" || note == "조사파일없음" {
			w("%s\t%d\t%s\t-\t-\t-\t-\t%s\n", label, len(g.hosts), fn, note)
			if note == "조사파일없음" {
				writeOnly(od, label, g.sets, len(g.hosts), ignored)
			}
			continue
		}
		var rows [][4]string
		rset := map[string]bool{}
		for _, n := range rnames {
			if !ignored(n) {
				rset[n] = true
			}
		}
		nr := map[string]string{}
		for n := range rset {
			nr[norm(n)] = n
		}
		na := map[string]string{}
		cnt := [3]int{}
		nign := 0
		for n, c := range g.sets {
			if ignored(n) {
				nign++
				continue
			}
			na[norm(n)] = n
			hc := fmt.Sprintf("%d/%d", c, len(g.hosts))
			if rset[n] {
				rows = append(rows, [4]string{n, "일치", hc, ""})
				cnt[0]++
			} else {
				rows = append(rows, [4]string{n, "실제만", hc, nr[norm(n)]})
				cnt[1]++
			}
		}
		for n := range rset {
			if g.sets[n] == 0 {
				rows = append(rows, [4]string{n, "조사만", "0/" + fmt.Sprint(len(g.hosts)), na[norm(n)]})
				cnt[2]++
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i][1] != rows[j][1] {
				return rows[i][1] < rows[j][1]
			}
			return rows[i][0] < rows[j][0]
		})
		tf, _ := os.Create(filepath.Join(od, label+".tsv"))
		fmt.Fprintln(tf, "이름\t상태\t보유호스트\t유사이름")
		for _, r := range rows {
			fmt.Fprintf(tf, "%s\t%s\t%s\t%s\n", r[0], r[1], r[2], r[3])
		}
		tf.Close()
		w("%s\t%d\t%s\t%d\t%d\t%d\t%d\t%s\n", label, len(g.hosts), fn, cnt[0], cnt[1], cnt[2], nign, note)
	}
	if len(skipped) > 0 {
		w("\n[건너뜀]\n")
		for _, s := range skipped {
			w("  %s\n", s)
		}
	}
	sm.Close()
	fmt.Printf("\n결과: %s/ (summary.txt, mapping.tsv, <모델>.tsv)\n", od)
}

// 조사파일이 없는 폴더: 실제조사 이름 목록만 저장한다.
func writeOnly(od, label string, set map[string]int, n int, ignored func(string) bool) {
	tf, _ := os.Create(filepath.Join(od, label+".tsv"))
	defer tf.Close()
	fmt.Fprintln(tf, "이름\t상태\t보유호스트\t유사이름")
	var ks []string
	for k := range set {
		if !ignored(k) {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(tf, "%s\t조사파일없음\t%d/%d\t\n", k, set[k], n)
	}
}
