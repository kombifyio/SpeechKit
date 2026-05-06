package main

type desktopCleanupStack struct {
	funcs []func()
}

func (s *desktopCleanupStack) Add(fn func()) {
	if fn == nil {
		return
	}
	s.funcs = append(s.funcs, fn)
}

func (s *desktopCleanupStack) Close() {
	for i := len(s.funcs) - 1; i >= 0; i-- {
		s.funcs[i]()
	}
	s.funcs = nil
}
