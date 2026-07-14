package logsafe

import (
	"strings"
)

func Reason(err error) string {
	if err == nil {
		return "unknown"
	}
	return Message(err.Error())
}

func Message(message string) string {
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.TrimSpace(message)
	if message == "" {
		return "unknown"
	}

	message = sanitizePathSpans(message)
	parts := strings.Fields(message)
	for index, part := range parts {
		parts[index] = sanitizeToken(part)
	}
	return strings.Join(parts, " ")
}

func FileName(path string) string {
	name := strings.TrimSpace(path)
	name = strings.Trim(name, `"'`)
	name = strings.TrimRight(name, `\/`)
	if index := strings.LastIndexAny(name, `\/`); index >= 0 {
		name = name[index+1:]
	}
	if name == "." || name == "" {
		return "unknown"
	}
	return name
}

func sanitizeToken(token string) string {
	core := token
	suffix := ""
	for len(core) > 0 {
		last := core[len(core)-1]
		if last != ':' && last != ';' && last != ',' && last != '.' {
			break
		}
		suffix = string(last) + suffix
		core = core[:len(core)-1]
	}
	if strings.ContainsAny(core, `/\`) {
		core = FileName(core)
	}
	if core == "" {
		core = "unknown"
	}
	return core + suffix
}

func sanitizePathSpans(message string) string {
	var builder strings.Builder
	builder.Grow(len(message))
	for index := 0; index < len(message); {
		if !isPathStart(message, index) {
			builder.WriteByte(message[index])
			index++
			continue
		}

		end := pathSpanEnd(message, index)
		builder.WriteString(FileName(message[index:end]))
		index = end
	}
	return builder.String()
}

func pathSpanEnd(message string, start int) int {
	for index := start; index < len(message); index++ {
		// Only the double quote terminates a path span. The apostrophe is a
		// valid character in Windows user and folder names (O'Brien, D'Angelo,
		// Team's Files), so treating it as a terminator would cut the span early
		// and leak the trailing directory/user segments.
		if message[index] == '"' {
			return index
		}
		if message[index] == ':' && index > start+1 {
			if index+1 == len(message) || message[index+1] == ' ' || message[index+1] == '\t' {
				return index
			}
		}
	}
	return len(message)
}

func isPathStart(message string, index int) bool {
	if index+2 < len(message) && isASCIIAlpha(message[index]) && message[index+1] == ':' && isPathSeparator(message[index+2]) {
		return true
	}
	return index+1 < len(message) && isPathSeparator(message[index]) && isPathSeparator(message[index+1])
}

func isPathSeparator(value byte) bool {
	return value == '\\' || value == '/'
}

func isASCIIAlpha(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}
