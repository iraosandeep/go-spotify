package main

import (
	"fmt"
	"io"
	"os"
)

// Terminal narration, in the same style as vstream.
const (
	colorAccent = "179" // amber-yellow, matches the UI's #ebcb8b
	colorOk     = "108" // muted green
	colorErr    = "167" // muted red
	colorWarn   = "179"
	colorDim    = "245"
)

var (
	uiForcedColor *bool
	uiQuiet       = false
)

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func colorEnabled(w io.Writer) bool {
	if uiForcedColor != nil {
		return *uiForcedColor
	}
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return false
	}
	if v, ok := os.LookupEnv("FORCE_COLOR"); ok {
		return v != "0" && v != "false"
	}
	return os.Getenv("TERM") != "dumb" && isTTY(w)
}

func paint(text, code string, w io.Writer) string {
	if text == "" || !colorEnabled(w) {
		return text
	}
	return fmt.Sprintf("\033[38;5;%sm%s\033[0m", code, text)
}

func uiDim(t string, w io.Writer) string    { return paint(t, colorDim, w) }
func uiAccent(t string, w io.Writer) string { return paint(t, colorAccent, w) }

func uiStatus(role, msg, detail string, w io.Writer) {
	if uiQuiet {
		return
	}
	mark, fallback, code := "·", "[INFO]", colorDim
	switch role {
	case "ok":
		mark, fallback, code = "✓", "[OK]", colorOk
	case "err":
		mark, fallback, code = "✗", "[FAIL]", colorErr
	case "warn":
		mark, fallback, code = "!", "[WARN]", colorWarn
	}
	if !isTTY(w) {
		mark = fallback
	}
	line := paint(mark, code, w) + " " + msg
	if detail != "" {
		line += "  " + uiDim(detail, w)
	}
	fmt.Fprintln(w, line)
}

func uiHeading(title string, w io.Writer) {
	if uiQuiet {
		return
	}
	fmt.Fprintln(w)
	if colorEnabled(w) {
		title = "\033[1m" + title + "\033[0m"
	}
	fmt.Fprintln(w, title)
}

func uiKV(label, value string, width int, w io.Writer) {
	if uiQuiet {
		return
	}
	fmt.Fprintf(w, "  %s %s\n", uiDim(fmt.Sprintf("%-*s", width, label+":"), w), value)
}

func uiHint(text string, w io.Writer) {
	if uiQuiet {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, uiDim(text, w))
}
