package cmd

import (
	"io"
	"os"
)

// termSafe returns w with terminal control sequences removed when w is a terminal; a file or pipe gets the bytes untouched.
func termSafe(w *os.File) io.Writer {
	if fi, err := w.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return &stripWriter{w: w}
	}
	return w
}

// stripWriter drops escape sequences and control characters other than newline and tab. It keeps its place between writes, so a sequence split across two of them is still removed.
type stripWriter struct {
	w     io.Writer
	state int
}

const (
	stText = iota
	stEsc
	stCSI
	stString
	stStringEsc
	stC1
)

func (s *stripWriter) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p))
	for _, b := range p {
		out = s.step(out, b)
	}
	if len(out) > 0 {
		if _, err := s.w.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (s *stripWriter) step(out []byte, b byte) []byte {
	switch s.state {
	case stEsc:
		switch b {
		case '[':
			s.state = stCSI
		case ']', 'P', 'X', '^', '_':
			s.state = stString
		default:
			s.state = stText
		}
		return out
	case stCSI:
		if b >= 0x40 && b <= 0x7e {
			s.state = stText
		}
		return out
	case stString:
		switch b {
		case 0x07:
			s.state = stText
		case 0x1b:
			s.state = stStringEsc
		}
		return out
	case stStringEsc:
		if b == '\\' || b == 0x07 {
			s.state = stText
		} else if b != 0x1b {
			s.state = stString
		}
		return out
	case stC1:
		s.state = stText
		if b >= 0x80 && b <= 0x9f {
			return out
		}
		out = append(out, 0xc2)
	}
	switch {
	case b == 0x1b:
		s.state = stEsc
	case b == 0xc2:
		s.state = stC1
	case b == '\n' || b == '\t':
		out = append(out, b)
	case b < 0x20 || b == 0x7f:
	default:
		out = append(out, b)
	}
	return out
}

// stripControl removes the same sequences from a string.
func stripControl(s string) string {
	var sb stringSink
	w := &stripWriter{w: &sb}
	_, _ = w.Write([]byte(s))
	return string(sb)
}

type stringSink []byte

func (s *stringSink) Write(p []byte) (int, error) {
	*s = append(*s, p...)
	return len(p), nil
}
