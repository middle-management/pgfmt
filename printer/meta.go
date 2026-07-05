package printer

import "strings"

// segment is a slice of the input: either a run of SQL text or a single
// psql meta-command line (e.g. "\restrict ..." emitted by pg_dump).
type segment struct {
	meta bool
	text string
}

// splitMetaCommands splits input into SQL text and psql meta-command lines.
// pg_dump emits meta-commands such as \restrict, \unrestrict and \connect,
// which are psql instructions rather than SQL — libpg_query cannot parse
// them. A line whose first non-blank character is a backslash is treated as
// a meta-command, but only outside strings, quoted identifiers, comments and
// dollar-quoted bodies.
func splitMetaCommands(sql string) []segment {
	if strings.IndexByte(sql, '\\') == -1 {
		return []segment{{text: sql}}
	}

	const (
		stNormal = iota
		stString       // '...'
		stIdent        // "..."
		stLineComment  // -- ...
		stBlockComment // /* ... */
		stDollar       // $tag$ ... $tag$
	)

	var segs []segment
	state := stNormal
	depth := 0       // block comment nesting
	tag := ""        // dollar-quote delimiter, including both $
	escapes := false // current string is E'...' (backslash escapes)
	segStart := 0
	atLineStart := true

	i := 0
	for i < len(sql) {
		c := sql[i]

		switch state {
		case stNormal:
			switch {
			case c == '\\' && atLineStart:
				lineEnd := len(sql)
				if nl := strings.IndexByte(sql[i:], '\n'); nl != -1 {
					lineEnd = i + nl + 1
				}
				if segStart < i {
					segs = append(segs, segment{text: sql[segStart:i]})
				}
				segs = append(segs, segment{meta: true, text: sql[i:lineEnd]})
				segStart = lineEnd
				i = lineEnd
				atLineStart = true
				continue
			case c == '\'':
				state = stString
				escapes = i > 0 && (sql[i-1] == 'E' || sql[i-1] == 'e') &&
					(i < 2 || !isIdentByte(sql[i-2]))
			case c == '"':
				state = stIdent
			case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
				state = stLineComment
				i++
			case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
				state = stBlockComment
				depth = 1
				i++
			case c == '$':
				if t := dollarTag(sql[i:]); t != "" {
					state = stDollar
					tag = t
					i += len(t)
					atLineStart = false
					continue
				}
			}
		case stString:
			switch {
			case escapes && c == '\\':
				i += 2
				continue
			case c == '\'':
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i += 2
					continue
				}
				state = stNormal
			}
		case stIdent:
			if c == '"' {
				if i+1 < len(sql) && sql[i+1] == '"' {
					i += 2
					continue
				}
				state = stNormal
			}
		case stLineComment:
			if c == '\n' {
				state = stNormal
			}
		case stBlockComment:
			if c == '/' && i+1 < len(sql) && sql[i+1] == '*' {
				depth++
				i++
			} else if c == '*' && i+1 < len(sql) && sql[i+1] == '/' {
				depth--
				i++
				if depth == 0 {
					state = stNormal
				}
			}
		case stDollar:
			if strings.HasPrefix(sql[i:], tag) {
				i += len(tag)
				state = stNormal
				atLineStart = false
				continue
			}
		}

		if c == '\n' {
			atLineStart = true
		} else if c != ' ' && c != '\t' && c != '\r' {
			atLineStart = false
		}
		i++
	}

	if segStart < len(sql) {
		segs = append(segs, segment{text: sql[segStart:]})
	}
	if len(segs) == 0 {
		segs = []segment{{text: sql}}
	}
	return segs
}

func isIdentByte(c byte) bool {
	return isIdentStartByte(c) || c >= '0' && c <= '9'
}

func isIdentStartByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// dollarTag returns the dollar-quote delimiter ("$$", "$tag$") at the start
// of s, or "" if s does not start one (e.g. a "$1" parameter).
func dollarTag(s string) string {
	j := 1
	if j < len(s) && isIdentStartByte(s[j]) {
		for j < len(s) && isIdentByte(s[j]) {
			j++
		}
	}
	if j < len(s) && s[j] == '$' {
		return s[:j+1]
	}
	return ""
}
