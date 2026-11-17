package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	protoVersion   = "2" // 평문 텍스트 (textproto.go)
	protoVersionV1 = "1" // gzip+base64 (읽기만 지원)
	blockKey       = "awx_copy"

	stateSending = "sending"
	stateAck     = "ack"

	encPlain = "base64"
	encGzip  = "gzip+base64"
)

// blockKeys 는 variables 블록에 쓰는 키 순서다.
// chunk_text(리터럴 블록) 뒤에 항상 다른 키 줄이 오게 한다. AWX 는 variables 끝 공백/줄바꿈을 지운다.
// chunk_data 는 v1 전용이며 값이 있을 때만 쓴다.
var blockKeys = []string{
	"version", "session", "state", "file_name", "type", "encoding", "eol", "final_newline",
	"total_bytes", "sha256", "chunk_total", "chunk_index", "chunk_sha256", "chunk_raw", "chunk_data",
	"chunk_text", "ack", "backup_b64", "updated_at",
}

// Block 은 variables 의 awx_copy 블록이다. 값은 모두 문자열이다.
type Block map[string]string

func (b Block) clone() Block {
	n := make(Block, len(b))
	for k, v := range b {
		n[k] = v
	}
	return n
}

func (b Block) int(key string) int {
	n, err := strconv.Atoi(b[key])
	if err != nil {
		return -1
	}
	return n
}

// yamlQuote 는 YAML 큰따옴표 문자열을 만든다. JSON 문자열 이스케이프는 YAML 큰따옴표 문법의 부분집합이다.
func yamlQuote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

// Render 는 YAML 로 읽히는 variables 문자열을 만든다.
func (b Block) Render() string {
	var sb strings.Builder
	sb.WriteString(blockKey + ":\n")
	for _, k := range blockKeys {
		if k == "chunk_data" && b[k] == "" {
			continue
		}
		sb.WriteString("  ")
		sb.WriteString(k)
		sb.WriteString(": ")
		if k == "chunk_text" {
			renderLiteral(&sb, b[k])
			continue
		}
		sb.WriteString(yamlQuote(b[k]))
		sb.WriteString("\n")
	}
	return sb.String()
}

var blockStartRe = regexp.MustCompile(`(?m)^awx_copy:\s*$`)
var blockJSONRe = regexp.MustCompile(`"awx_copy"\s*:`)

// hasBlock 은 variables 에 awx_copy 블록(어떤 세션이든)이 있는지 본다.
func hasBlock(vars string) bool {
	return blockStartRe.MatchString(vars) || blockJSONRe.MatchString(vars)
}

// parseBlock 은 Render 가 만든 형식(또는 AWX 가 JSON 으로 바꾼 형식)을 읽는다.
func parseBlock(vars string) (Block, bool) {
	if !hasBlock(vars) {
		return nil, false
	}
	t := strings.TrimSpace(vars)
	if strings.HasPrefix(t, "{") {
		var m map[string]map[string]interface{}
		if err := json.Unmarshal([]byte(t), &m); err == nil {
			if inner, ok := m[blockKey]; ok {
				b := Block{}
				for k, v := range inner {
					b[k] = fmt.Sprint(v)
				}
				return b, b["session"] != ""
			}
		}
	}
	loc := blockStartRe.FindStringIndex(vars)
	if loc == nil {
		return nil, false
	}
	b := Block{}
	lines := strings.Split(vars[loc[1]:], "\n")
	for li := 0; li < len(lines); li++ {
		line := strings.TrimRight(lines[li], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			break
		}
		ind := len(line) - len(strings.TrimLeft(line, " "))
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, ": ")
		if i < 0 {
			continue
		}
		k, raw := line[:i], strings.TrimSpace(line[i+2:])
		var v string
		if strings.HasPrefix(raw, "|") {
			val, next, err := parseLiteral(lines, li+1, ind, raw)
			if err != nil {
				return nil, false
			}
			b[k] = val
			li = next - 1
			continue
		}
		if strings.HasPrefix(raw, "\"") {
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				return nil, false
			}
		} else {
			v = strings.Trim(raw, "'")
		}
		b[k] = v
	}
	return b, b["session"] != ""
}

func newSession() string {
	var r [8]byte
	_, _ = rand.Read(r[:])
	return time.Now().Format("20060102T150405") + "-" + hex.EncodeToString(r[:])
}

func nowStamp() string { return time.Now().Format(time.RFC3339) }

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// encodePayload 는 gzip 이 이득일 때만 압축한다.
func encodePayload(data []byte) ([]byte, string) {
	if len(data) == 0 {
		return data, encPlain
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	if buf.Len() < len(data) {
		return buf.Bytes(), encGzip
	}
	return data, encPlain
}

func decodePayload(b64, enc string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("base64 해독 실패: %v", err)
	}
	switch enc {
	case encPlain:
		return raw, nil
	case encGzip:
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("gzip 해제 실패: %v", err)
		}
		defer zr.Close()
		out, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("gzip 해제 실패: %v", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("알 수 없는 encoding %q", enc)
	}
}

// splitChunks 는 base64 문자열을 조각으로 나눈다. 빈 문자열도 조각 1개다.
func splitChunks(s string, size int) []string {
	if size <= 0 || len(s) <= size {
		return []string{s}
	}
	var out []string
	for i := 0; i < len(s); i += size {
		j := i + size
		if j > len(s) {
			j = len(s)
		}
		out = append(out, s[i:j])
	}
	return out
}
