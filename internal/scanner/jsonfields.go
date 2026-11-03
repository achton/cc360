package scanner

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// topLevelFields walks the top level of one JSON object and hands each key and
// its raw value to fn. It does not validate what it skips. Transcripts are
// mostly long strings, and encoding/json checking every byte of them was
// nearly all of the scan time, so string contents are jumped over with
// IndexByte instead. Returns false for anything that is not a well-formed
// object at the top level.
func topLevelFields(b []byte, fn func(key, val []byte)) bool {
	i := skipWS(b, 0)
	if i >= len(b) || b[i] != '{' {
		return false
	}
	i = skipWS(b, i+1)
	if i < len(b) && b[i] == '}' {
		return true
	}
	for i < len(b) {
		if b[i] != '"' {
			return false
		}
		end := skipString(b, i)
		if end < 0 {
			return false
		}
		key := b[i+1 : end-1]
		if bytes.IndexByte(key, '\\') >= 0 {
			// Valid JSON may escape a key, e.g. "\u0063wd" for "cwd".
			key = []byte(jsonString(b[i:end]))
		}
		i = skipWS(b, end)
		if i >= len(b) || b[i] != ':' {
			return false
		}
		i = skipWS(b, i+1)
		vEnd := skipValue(b, i)
		if vEnd <= i {
			return false
		}
		fn(key, b[i:vEnd])
		i = skipWS(b, vEnd)
		if i >= len(b) {
			return false
		}
		if b[i] == '}' {
			return true
		}
		if b[i] != ',' {
			return false
		}
		i = skipWS(b, i+1)
	}
	return false
}

func isWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func skipWS(b []byte, i int) int {
	for i < len(b) && isWS(b[i]) {
		i++
	}
	return i
}

// skipString returns the index just past the string that opens at b[i], or -1.
func skipString(b []byte, i int) int {
	i++
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return -1
		}
		end := i + j
		// The quote is escaped when an odd run of backslashes precedes it.
		k := end - 1
		for k >= i && b[k] == '\\' {
			k--
		}
		if (end-1-k)%2 == 0 {
			return end + 1
		}
		i = end + 1
	}
}

// skipValue returns the index just past the value that starts at b[i], or -1.
func skipValue(b []byte, i int) int {
	if i >= len(b) {
		return -1
	}
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				i = skipString(b, i)
				if i < 0 {
					return -1
				}
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return -1
	default:
		for i < len(b) && b[i] != ',' && b[i] != '}' && b[i] != ']' && !isWS(b[i]) {
			i++
		}
		return i
	}
}

// jsonString decodes a raw JSON string value, or returns "" for anything else.
func jsonString(v []byte) string {
	if len(v) < 2 || v[0] != '"' {
		return ""
	}
	inner := v[1 : len(v)-1]
	if bytes.IndexByte(inner, '\\') < 0 {
		return string(inner)
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

func jsonTrue(v []byte) bool {
	return string(v) == "true"
}

func jsonInt(v []byte) int64 {
	n, _ := strconv.ParseInt(string(v), 10, 64)
	return n
}

func jsonFloat(v []byte) float64 {
	f, _ := strconv.ParseFloat(string(v), 64)
	return f
}

// decodeEntry fills e from one transcript record. e.Message aliases line.
func decodeEntry(line []byte, e *jsonlEntry) bool {
	*e = jsonlEntry{}
	return topLevelFields(line, func(k, v []byte) {
		switch string(k) {
		case "type":
			e.Type = jsonString(v)
		case "sessionId":
			e.SessionID = jsonString(v)
		case "cwd":
			e.Cwd = jsonString(v)
		case "gitBranch":
			e.GitBranch = jsonString(v)
		case "isSidechain":
			e.IsSidechain = jsonTrue(v)
		case "isMeta":
			e.IsMeta = jsonTrue(v)
		case "isCompactSummary":
			e.IsCompactSummary = jsonTrue(v)
		case "timestamp":
			e.Timestamp = jsonString(v)
		case "aiTitle":
			e.AITitle = jsonString(v)
		case "customTitle":
			e.CustomTitle = jsonString(v)
		case "agentName":
			e.AgentName = jsonString(v)
		case "entrypoint":
			e.Entrypoint = jsonString(v)
		case "prNumber":
			e.PRNumber = jsonInt(v)
		case "prRepository":
			e.PRRepository = jsonString(v)
		case "totalCostUSD":
			e.TotalCostUSD = jsonFloat(v)
		case "totalLinesAdded":
			e.LinesAdded = jsonInt(v)
		case "totalLinesRemoved":
			e.LinesRemoved = jsonInt(v)
		case "message":
			e.Message = v
		}
	})
}
