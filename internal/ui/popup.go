package ui

// что видно в момент проверки всплывающего окна.
type popupProbe struct {
	ButtonDown       bool
	CursorInside     bool
	Foreground, Self uintptr
}

type popupWatch struct {
	armed  bool
	active bool
}

func (p *popupWatch) shouldClose(in popupProbe) bool {
	switch {
	case !in.ButtonDown:
		p.armed = true
	case p.armed && !in.CursorInside:
		return true
	}

	switch {
	case in.Foreground == in.Self:
		p.active = true
	case p.active && in.Foreground != 0:
		return true
	}
	return false
}
