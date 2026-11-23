// dcgm_dep.go - scp 서버의 DCGM rpm 이 요구하는 부족한 의존성을 rpm -U --test 로 찾고,
// 관리 서버의 같은 baseurl 경로 repodata(primary.xml.gz)에서 제공 rpm 을 찾는다.
package main

import (
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// maxDepRounds 는 "rpm --test → 의존성 rpm 전송" 을 되풀이하는 최대 횟수다.
const maxDepRounds = 10

// rpmTestScript 는 보낸 rpm 으로 설치했을 때 부족한 의존성을 rpm 이 출력하게 한다(설치는 하지 않는다).
func rpmTestScript(files []string) string {
	var q []string
	for _, f := range files {
		q = append(q, shQuote(f))
	}
	return "LC_ALL=C rpm -U --test " + strings.Join(q, " ") + " 2>&1" // 한국어 로케일이면 메시지가 번역돼 파싱 불가
}

// parseMissingDeps 는 rpm -U --test 출력의 "X is needed by Y" 줄에서 capability 이름 X(버전 조건 앞)를
// 중복 없이 정렬해 돌려준다. "conflicts with" 같은 줄(반대 CUDA 패키지 충돌 등)은 무시한다.
func parseMissingDeps(out string) []string {
	seen := map[string]bool{}
	var caps []string
	for _, line := range strings.Split(out, "\n") {
		c, _, ok := strings.Cut(strings.TrimSpace(line), " is needed by ")
		if !ok || c == "" {
			continue
		}
		if !strings.HasPrefix(c, "(") { // (a or b) 같은 rich 의존성은 통째로 둔다
			c = strings.Fields(c)[0]
		}
		if !seen[c] {
			seen[c] = true
			caps = append(caps, c)
		}
	}
	sort.Strings(caps)
	return caps
}

// repoPkg 는 primary.xml 의 package 하나에서 필요한 부분이다.
type repoPkg struct {
	Name    string `xml:"name"`
	Arch    string `xml:"arch"`
	Version struct {
		Epoch string `xml:"epoch,attr"`
		Ver   string `xml:"ver,attr"`
		Rel   string `xml:"rel,attr"`
	} `xml:"version"`
	Location struct {
		Href string `xml:"href,attr"`
	} `xml:"location"`
	Provides []struct {
		Name string `xml:"name,attr"`
	} `xml:"format>provides>entry"`
	Files []string `xml:"format>file"`
}

// depProvider 는 capability 를 제공하는 rpm 이다.
type depProvider struct {
	path  string // 관리 서버 경로
	epoch int
	evr   string // "버전-릴리스"
}

// repoIndex 는 capability(provides·파일 경로) → 제공 rpm 목록이다.
type repoIndex map[string][]depProvider

// repomdPrimary 는 repomd.xml 에서 type="primary" 데이터의 location href 를 읽는다.
func repomdPrimary(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var md struct {
		Data []struct {
			Type     string `xml:"type,attr"`
			Location struct {
				Href string `xml:"href,attr"`
			} `xml:"location"`
		} `xml:"data"`
	}
	if err := xml.NewDecoder(f).Decode(&md); err != nil {
		return "", err
	}
	for _, d := range md.Data {
		if d.Type == "primary" && d.Location.Href != "" {
			return d.Location.Href, nil
		}
	}
	return "", fmt.Errorf("primary not in repomd.xml")
}

// addRepoIndex 는 repo 디렉토리 dir 의 primary.xml.gz 를 읽어 idx 에 더한다. x86_64/noarch 만 쓴다(src 제외).
func addRepoIndex(idx repoIndex, dir string) error {
	href, err := repomdPrimary(filepath.Join(dir, "repodata", "repomd.xml"))
	if err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(href)))
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(href, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "package" {
			continue
		}
		var p repoPkg
		if err := dec.DecodeElement(&p, &se); err != nil {
			return err
		}
		if (p.Arch != "x86_64" && p.Arch != "noarch") || p.Location.Href == "" {
			continue
		}
		ep, _ := strconv.Atoi(p.Version.Epoch)
		dp := depProvider{filepath.Join(dir, filepath.FromSlash(p.Location.Href)), ep, p.Version.Ver + "-" + p.Version.Rel}
		for _, e := range p.Provides {
			idx[e.Name] = append(idx[e.Name], dp)
		}
		for _, n := range p.Files {
			idx[n] = append(idx[n], dp)
		}
	}
}

// LoadRepoIndex 는 baseurl(file://) 경로들의 repodata 를 읽는다. repomd.xml 이 있는 경로가 하나도 없으면
// fail 사유("dcgm no repodata")를 오류로 낸다.
func LoadRepoIndex(baseURLs []string) (repoIndex, error) {
	dirs, _ := repoDirs(baseURLs)
	idx := repoIndex{}
	loaded := 0
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "repodata", "repomd.xml")); err != nil {
			continue
		}
		if err := addRepoIndex(idx, d); err != nil {
			return nil, fmt.Errorf("dcgm no repodata")
		}
		loaded++
	}
	if loaded == 0 {
		return nil, fmt.Errorf("dcgm no repodata")
	}
	return idx, nil
}

// find 는 capability 를 제공하는 rpm 중 skip 에 없는 가장 새 것의 경로를 돌려준다. 없으면 "".
func (idx repoIndex) find(capName string, skip map[string]bool) string {
	var best *depProvider
	for i, p := range idx[capName] {
		if skip[p.path] {
			continue
		}
		if best == nil || p.epoch > best.epoch || p.epoch == best.epoch && rpmEVRCmp(p.evr, best.evr) > 0 {
			best = &idx[capName][i]
		}
	}
	if best == nil {
		return ""
	}
	return best.path
}

// resolveDeps 는 caps 각각의 제공 rpm 경로(중복 없이)와 못 찾은 capability 를 돌려준다.
// skip 은 이미 보낸 rpm 이다(보냈는데도 부족하면 같은 것을 또 고르지 않는다).
func (idx repoIndex) resolveDeps(caps []string, skip map[string]bool) (files, missing []string) {
	got := map[string]bool{}
	for _, c := range caps {
		p := idx.find(c, skip)
		switch {
		case p == "":
			missing = append(missing, c)
		case !got[p]:
			got[p] = true
			files = append(files, p)
		}
	}
	return
}
